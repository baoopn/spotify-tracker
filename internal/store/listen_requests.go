package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Listen request statuses.
const (
	RequestPending   = "pending"
	RequestInvited   = "invited"
	RequestDismissed = "dismissed"
)

var (
	// ErrRequestsFull means too many requests are pending; nothing was stored.
	ErrRequestsFull = errors.New("too many pending listen requests")
	// ErrRequestNotPending means the request was already invited or dismissed.
	ErrRequestNotPending = errors.New("listen request is not pending")
)

// listenRequestLockKey serializes request inserts ("lstnreq" as an int64),
// so the pending cap holds under concurrent submissions.
const listenRequestLockKey int64 = 0x6c73746e726571

// ListenRequest is a visitor's request to be invited to listen along.
type ListenRequest struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	Message   *string    `json:"message"`
	Status    string     `json:"status"`
	Country   *string    `json:"country"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DecidedAt *time.Time `json:"decided_at"`
	DecidedBy *string    `json:"decided_by"`
	// AlreadyListener is true when a live listeners row has this email.
	AlreadyListener bool `json:"already_listener"`
}

// NewListenRequest is a validated submission.
type NewListenRequest struct {
	Name, Email string
	Message     *string
	Country     *string
}

const listenRequestColumns = `r.id, r.name, r.email, r.message, r.status, r.country, r.created_at, r.updated_at,
	r.decided_at, r.decided_by,
	EXISTS (SELECT 1 FROM listeners l WHERE l.email = r.email AND l.status <> 'revoked')`

func scanListenRequest(row pgx.Row) (ListenRequest, error) {
	var x ListenRequest
	err := row.Scan(&x.ID, &x.Name, &x.Email, &x.Message, &x.Status, &x.Country, &x.CreatedAt, &x.UpdatedAt,
		&x.DecidedAt, &x.DecidedBy, &x.AlreadyListener)
	x.CreatedAt, x.UpdatedAt = x.CreatedAt.UTC(), x.UpdatedAt.UTC()
	if x.DecidedAt != nil {
		t := x.DecidedAt.UTC()
		x.DecidedAt = &t
	}
	return x, err
}

// SubmitListenRequest stores a request. For an email with a pending request
// it keeps that request's name and message (filling only an empty message or
// country) and moves updated_at (created is false); otherwise it adds a new
// one unless maxPending requests are already pending (ErrRequestsFull).
func (s *Store) SubmitListenRequest(ctx context.Context, n NewListenRequest, maxPending int) (ListenRequest, bool, error) {
	var out ListenRequest
	created := false
	err := s.InTx(ctx, func(q *Queries) error {
		if _, err := q.db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, listenRequestLockKey); err != nil {
			return fmt.Errorf("lock listen requests: %w", err)
		}
		// Anyone can submit any email, so a resubmission never replaces what
		// the first one said: it only fills empty fields.
		r, err := scanListenRequest(q.db.QueryRow(ctx, `UPDATE listen_requests r SET message = COALESCE(r.message, $2),
				country = COALESCE(r.country, $3), updated_at = now()
			WHERE r.email = $1 AND r.status = 'pending' RETURNING `+listenRequestColumns, n.Email, n.Message, n.Country))
		if err == nil {
			out = r
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("update listen request: %w", err)
		}
		var pending int
		if err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM listen_requests WHERE status = 'pending'`).Scan(&pending); err != nil {
			return fmt.Errorf("count listen requests: %w", err)
		}
		if pending >= maxPending {
			return ErrRequestsFull
		}
		r, err = scanListenRequest(q.db.QueryRow(ctx, `INSERT INTO listen_requests AS r (name, email, message, country)
			VALUES ($1, $2, $3, $4) RETURNING `+listenRequestColumns, n.Name, n.Email, n.Message, n.Country))
		if err != nil {
			return fmt.Errorf("insert listen request: %w", err)
		}
		out, created = r, true
		return nil
	})
	return out, created, err
}

// ListListenRequests lists requests with status ("pending", "dismissed" or
// "all"), newest first (at most 200), and the number pending.
func (q *Queries) ListListenRequests(ctx context.Context, status string) ([]ListenRequest, int64, error) {
	where := "r.status = $1"
	if status == "all" {
		where = "$1 = 'all'"
	}
	rows, err := q.db.Query(ctx, `SELECT `+listenRequestColumns+` FROM listen_requests r WHERE `+where+`
		ORDER BY r.created_at DESC, r.id DESC LIMIT 200`, status)
	if err != nil {
		return nil, 0, fmt.Errorf("list listen requests: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ListenRequest, error) { return scanListenRequest(r) })
	if err != nil {
		return nil, 0, fmt.Errorf("list listen requests: %w", err)
	}
	if out == nil {
		out = []ListenRequest{}
	}
	n, err := q.PendingListenRequests(ctx)
	return out, n, err
}

// PendingListenRequests counts the pending requests.
func (q *Queries) PendingListenRequests(ctx context.Context) (int64, error) {
	var n int64
	if err := q.db.QueryRow(ctx, `SELECT COUNT(*) FROM listen_requests WHERE status = 'pending'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count listen requests: %w", err)
	}
	return n, nil
}

// GetListenRequest returns one request, or domain.ErrNotFound.
func (q *Queries) GetListenRequest(ctx context.Context, id int64) (ListenRequest, error) {
	r, err := scanListenRequest(q.db.QueryRow(ctx, `SELECT `+listenRequestColumns+` FROM listen_requests r WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ListenRequest{}, domain.ErrNotFound
	}
	if err != nil {
		return ListenRequest{}, fmt.Errorf("get listen request: %w", err)
	}
	return r, nil
}

// DecideListenRequest marks a pending request invited or dismissed by an
// admin: domain.ErrNotFound, or ErrRequestNotPending when it was decided.
func (q *Queries) DecideListenRequest(ctx context.Context, id int64, status, by string) (ListenRequest, error) {
	r, err := scanListenRequest(q.db.QueryRow(ctx, `UPDATE listen_requests r
		SET status = $2, decided_at = now(), decided_by = $3, updated_at = now()
		WHERE r.id = $1 AND r.status = 'pending' AND $2 IN ('invited', 'dismissed')
		RETURNING `+listenRequestColumns, id, status, by))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := q.GetListenRequest(ctx, id); gerr != nil {
			return ListenRequest{}, gerr
		}
		return ListenRequest{}, ErrRequestNotPending
	}
	if err != nil {
		return ListenRequest{}, fmt.Errorf("decide listen request: %w", err)
	}
	return r, nil
}

// PruneListenRequests deletes decided requests older than age.
func (q *Queries) PruneListenRequests(ctx context.Context, age time.Duration) (int64, error) {
	tag, err := q.db.Exec(ctx, `DELETE FROM listen_requests WHERE status <> 'pending' AND decided_at < $1`,
		time.Now().Add(-age))
	if err != nil {
		return 0, fmt.Errorf("prune listen requests: %w", err)
	}
	return tag.RowsAffected(), nil
}
