package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/backupstatus"
	"github.com/baoopn/spotify-tracker/internal/domain"
)

// The backup container (deploy/backup) writes backup_runs, backup_inventory
// and backup_agent; the API only reads them and queues backup_requests.

const backupRunColumns = `id, trigger, status, started_at, finished_at, name, size_bytes, sha256,
	tables, error, requested_by`

func backupRunTargets(r *domain.BackupRun) []any {
	return []any{&r.ID, &r.Trigger, &r.Status, &r.StartedAt, &r.FinishedAt, &r.Name, &r.SizeBytes,
		&r.SHA256, &r.Tables, &r.Error, &r.RequestedBy}
}

// PendingRequestMaxAge is how old an unfinished request may be and still
// count as pending. Older ones (an agent that died mid-request) no longer
// block new requests.
const PendingRequestMaxAge = 6 * time.Hour

// backupRequestLockKey serializes request inserts ("bkreqst" as an int64).
const backupRequestLockKey int64 = 0x626b7265717374

// BackupAgent returns the backup container's heartbeat, or nil when it
// never ran.
func (q *Queries) BackupAgent(ctx context.Context) (*domain.BackupAgent, error) {
	var a domain.BackupAgent
	err := q.db.QueryRow(ctx, `SELECT last_seen_at, version, configured, config_error, destination,
			COALESCE(recipients, '{}'), backup_time, keep_days, keep_min, paused, next_run_at,
			COALESCE(settings_source, 'env')
		FROM backup_agent WHERE id = 1`).Scan(&a.LastSeenAt, &a.Version, &a.Configured, &a.ConfigError,
		&a.Destination, &a.Recipients, &a.BackupTime, &a.KeepDays, &a.KeepMin, &a.Paused, &a.NextRunAt,
		&a.SettingsSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup agent: %w", err)
	}
	return &a, nil
}

// BackupRuns lists backup runs, newest first, with the total count.
func (q *Queries) BackupRuns(ctx context.Context, limit, offset int) ([]domain.BackupRun, int, error) {
	var total int
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM backup_runs`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("backup runs count: %w", err)
	}
	rows, err := q.db.Query(ctx, `SELECT `+backupRunColumns+` FROM backup_runs
		ORDER BY started_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("backup runs: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.BackupRun, error) {
		var b domain.BackupRun
		err := r.Scan(backupRunTargets(&b)...)
		return b, err
	})
	if out == nil {
		out = []domain.BackupRun{}
	}
	return out, total, err
}

// LatestBackupRun returns the newest run with the given status, or nil.
func (q *Queries) LatestBackupRun(ctx context.Context, status string) (*domain.BackupRun, error) {
	var b domain.BackupRun
	err := q.db.QueryRow(ctx, `SELECT `+backupRunColumns+` FROM backup_runs
		WHERE status = $1 ORDER BY started_at DESC, id DESC LIMIT 1`, status).Scan(backupRunTargets(&b)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("latest %s backup run: %w", status, err)
	}
	return &b, nil
}

// BackupInventory lists the stored backups (the container's latest listing
// snapshot), newest first.
func (q *Queries) BackupInventory(ctx context.Context) ([]domain.BackupObject, error) {
	rows, err := q.db.Query(ctx, `SELECT name, size_bytes, created_at FROM backup_inventory
		ORDER BY created_at DESC, name DESC LIMIT 10000`)
	if err != nil {
		return nil, fmt.Errorf("backup inventory: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.BackupObject, error) {
		var o domain.BackupObject
		err := r.Scan(&o.Name, &o.SizeBytes, &o.CreatedAt)
		return o, err
	})
	if out == nil {
		out = []domain.BackupObject{}
	}
	return out, err
}

// BackupState loads what the backup health is computed from.
func (q *Queries) BackupState(ctx context.Context) (backupstatus.State, error) {
	var s backupstatus.State
	var err error
	if s.Agent, err = q.BackupAgent(ctx); err != nil {
		return s, err
	}
	if s.LastSuccess, err = q.LatestBackupRun(ctx, domain.BackupSuccess); err != nil {
		return s, err
	}
	if s.LastFailure, err = q.LatestBackupRun(ctx, domain.BackupFailed); err != nil {
		return s, err
	}
	if err := q.db.QueryRow(ctx, `SELECT MIN(started_at) FROM backup_runs`).Scan(&s.FirstRunAt); err != nil {
		return s, fmt.Errorf("first backup run: %w", err)
	}
	return s, nil
}

const pendingRequestQuery = `SELECT id, kind, requested_at, requested_by, claimed_at
	FROM backup_requests
	WHERE done_at IS NULL AND requested_at > now() - make_interval(secs => $2)
		AND ($1::text = '' OR kind = $1)
	ORDER BY (kind = 'backup') DESC, requested_at, id
	LIMIT 1`

func pendingRequest(ctx context.Context, db DBTX, kind string) (*domain.BackupRequest, error) {
	var r domain.BackupRequest
	err := db.QueryRow(ctx, pendingRequestQuery, kind, PendingRequestMaxAge.Seconds()).
		Scan(&r.ID, &r.Kind, &r.RequestedAt, &r.RequestedBy, &r.ClaimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pending backup request: %w", err)
	}
	return &r, nil
}

// PendingBackupRequest returns the oldest unfinished request of kind ("" =
// any kind, backups first), or nil. Requests older than
// PendingRequestMaxAge are ignored.
func (q *Queries) PendingBackupRequest(ctx context.Context, kind string) (*domain.BackupRequest, error) {
	return pendingRequest(ctx, q.db, kind)
}

// CreateBackupRequest queues a request for the backup container unless one
// of the same kind is still pending. created is false (with the pending
// request's ID) when one is.
func (s *Store) CreateBackupRequest(ctx context.Context, kind, requestedBy string) (id int64, created bool, err error) {
	err = s.InTx(ctx, func(q *Queries) error {
		if _, err := q.db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, backupRequestLockKey); err != nil {
			return fmt.Errorf("backup request lock: %w", err)
		}
		cur, err := pendingRequest(ctx, q.db, kind)
		if err != nil {
			return err
		}
		if cur != nil {
			id = cur.ID
			return nil
		}
		if err := q.db.QueryRow(ctx, `INSERT INTO backup_requests (kind, requested_by)
			VALUES ($1, $2) RETURNING id`, kind, nullIfEmpty(requestedBy)).Scan(&id); err != nil {
			return fmt.Errorf("insert backup request: %w", err)
		}
		created = true
		return nil
	})
	return id, created, err
}
