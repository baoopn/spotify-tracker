package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/spotify-tracker/internal/domain"
)

// Listener statuses.
const (
	ListenerInvited = "invited"
	ListenerActive  = "active"
	ListenerRevoked = "revoked"
)

// Listener is one person invited to listen along at /listen.
type Listener struct {
	ID                int64
	Email             string
	Status            string
	ClerkUserID       *string
	ClerkInvitationID *string
	InvitedBy         string
	InvitedAt         time.Time
	AcceptedAt        *time.Time
	RevokedAt         *time.Time
	LastSeenAt        *time.Time
}

// NewListener is a listener to invite.
type NewListener struct {
	Email             string
	InvitedBy         string
	ClerkUserID       *string
	ClerkInvitationID *string
}

var (
	// ErrListenerExists means a live (not revoked) row already has this
	// email or Clerk user.
	ErrListenerExists = errors.New("listener already exists")
	// ErrListenerRevoked means the listener is already revoked.
	ErrListenerRevoked = errors.New("listener already revoked")
)

const listenerColumns = `id, email, status, clerk_user_id, clerk_invitation_id, invited_by, invited_at,
	accepted_at, revoked_at, last_seen_at`

func scanListener(row pgx.Row) (Listener, error) {
	var l Listener
	err := row.Scan(&l.ID, &l.Email, &l.Status, &l.ClerkUserID, &l.ClerkInvitationID, &l.InvitedBy,
		&l.InvitedAt, &l.AcceptedAt, &l.RevokedAt, &l.LastSeenAt)
	return l, err
}

// listenerErr maps no rows to domain.ErrNotFound and a unique violation to
// ErrListenerExists.
func listenerErr(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrListenerExists
	}
	return fmt.Errorf("%s: %w", op, err)
}

// ListListeners lists listeners: active, then invited, then revoked, each
// newest invitation first (at most 500).
func (q *Queries) ListListeners(ctx context.Context) ([]Listener, error) {
	rows, err := q.db.Query(ctx, `SELECT `+listenerColumns+` FROM listeners
		ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'invited' THEN 1 ELSE 2 END, invited_at DESC, id DESC
		LIMIT 500`)
	if err != nil {
		return nil, fmt.Errorf("list listeners: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Listener, error) { return scanListener(r) })
	if err != nil {
		return nil, fmt.Errorf("list listeners: %w", err)
	}
	if out == nil {
		out = []Listener{}
	}
	return out, nil
}

// GetListener returns one listener, or domain.ErrNotFound.
func (q *Queries) GetListener(ctx context.Context, id int64) (Listener, error) {
	l, err := scanListener(q.db.QueryRow(ctx, `SELECT `+listenerColumns+` FROM listeners WHERE id = $1`, id))
	if err != nil {
		return Listener{}, listenerErr("get listener", err)
	}
	return l, nil
}

// CreateListener adds an invited listener. ErrListenerExists when a live row
// already has the email or Clerk user.
func (q *Queries) CreateListener(ctx context.Context, n NewListener) (Listener, error) {
	l, err := scanListener(q.db.QueryRow(ctx, `INSERT INTO listeners (email, invited_by, clerk_user_id, clerk_invitation_id)
		VALUES ($1, $2, $3, $4) RETURNING `+listenerColumns,
		strings.ToLower(strings.TrimSpace(n.Email)), n.InvitedBy, n.ClerkUserID, n.ClerkInvitationID))
	if err != nil {
		return Listener{}, listenerErr("create listener", err)
	}
	return l, nil
}

// SetListenerInvitation stores a new Clerk invitation ID on a still invited
// listener (domain.ErrNotFound otherwise).
func (q *Queries) SetListenerInvitation(ctx context.Context, id int64, invitationID string) (Listener, error) {
	l, err := scanListener(q.db.QueryRow(ctx, `UPDATE listeners SET clerk_invitation_id = $2
		WHERE id = $1 AND status = 'invited' RETURNING `+listenerColumns, id, invitationID))
	if err != nil {
		return Listener{}, listenerErr("set listener invitation", err)
	}
	return l, nil
}

// RevokeListener revokes a listener: domain.ErrNotFound when there is no
// such row, ErrListenerRevoked when it is already revoked.
func (q *Queries) RevokeListener(ctx context.Context, id int64) (Listener, error) {
	l, err := scanListener(q.db.QueryRow(ctx, `UPDATE listeners SET status = 'revoked', revoked_at = now()
		WHERE id = $1 AND status <> 'revoked' RETURNING `+listenerColumns, id))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, gerr := q.GetListener(ctx, id); gerr != nil {
			return Listener{}, gerr
		}
		return Listener{}, ErrListenerRevoked
	}
	if err != nil {
		return Listener{}, listenerErr("revoke listener", err)
	}
	return l, nil
}

// ListenerForUser finds the live listener row for a signed-in Clerk user:
// the row bound to clerkUserID, else an unbound row invited with email. An
// empty clerkUserID matches by email alone (bound or not), for duplicate
// checks; an empty email matches by user ID alone. domain.ErrNotFound when
// there is none.
func (q *Queries) ListenerForUser(ctx context.Context, clerkUserID, email string) (Listener, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	l, err := scanListener(q.db.QueryRow(ctx, `SELECT `+listenerColumns+` FROM listeners
		WHERE status <> 'revoked' AND (
			($1 <> '' AND clerk_user_id = $1) OR
			($2 <> '' AND email = $2 AND ($1 = '' OR clerk_user_id IS NULL)))
		ORDER BY (clerk_user_id = $1) DESC NULLS LAST, id DESC LIMIT 1`, clerkUserID, email))
	if err != nil {
		return Listener{}, listenerErr("listener for user", err)
	}
	return l, nil
}

// AcceptListener marks an invited listener active for clerkUserID. accepted
// is false when the row was not invited any more (or bound to another
// user); the current row is returned then.
func (q *Queries) AcceptListener(ctx context.Context, id int64, clerkUserID string) (Listener, bool, error) {
	l, err := scanListener(q.db.QueryRow(ctx, `UPDATE listeners
		SET status = 'active', clerk_user_id = $2, accepted_at = now(), last_seen_at = now()
		WHERE id = $1 AND status = 'invited' AND (clerk_user_id IS NULL OR clerk_user_id = $2)
		RETURNING `+listenerColumns, id, clerkUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		cur, gerr := q.GetListener(ctx, id)
		return cur, false, gerr
	}
	if err != nil {
		return Listener{}, false, listenerErr("accept listener", err)
	}
	return l, true, nil
}

// TouchListener records that a listener was seen, at most every 5 minutes.
func (q *Queries) TouchListener(ctx context.Context, id int64) error {
	_, err := q.db.Exec(ctx, `UPDATE listeners SET last_seen_at = now()
		WHERE id = $1 AND (last_seen_at IS NULL OR last_seen_at < now() - interval '5 minutes')`, id)
	if err != nil {
		return fmt.Errorf("touch listener: %w", err)
	}
	return nil
}
