package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// ListenerRole is the Clerk public_metadata.role given to invited
// listeners. It is a hint for Clerk only: the listeners table decides
// access, and this role never grants admin access.
const ListenerRole = "listener"

// listenerTouchEvery is how often last_seen_at is updated.
const listenerTouchEvery = 5 * time.Minute

// Listener is the signed-in person on /listen: an invited listener, or an
// admin (who may always listen along).
type Listener struct {
	ID       int64  `json:"-"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	ImageURL string `json:"image_url"`
	Admin    bool   `json:"admin"`
}

type listenerCtxKey struct{}

// ListenerFrom returns the listener stored in the request context.
func ListenerFrom(ctx context.Context) (Listener, bool) {
	l, ok := ctx.Value(listenerCtxKey{}).(Listener)
	return l, ok
}

// ListenerStore reads and updates listener rows (implemented by
// *store.Store).
type ListenerStore interface {
	ListenerForUser(ctx context.Context, clerkUserID, email string) (store.Listener, error)
	AcceptListener(ctx context.Context, id int64, clerkUserID string) (store.Listener, bool, error)
	TouchListener(ctx context.Context, id int64) error
	Audit(ctx context.Context, actor, action string, detail any, ip string) error
}

// ListenerMiddleware requires a valid Clerk session token for an admin or
// a listener with a live (invited or active) row. The row is read on every
// request, so a revoke takes effect on the next one. The first request of
// an invited listener marks the row active.
func (a *Authenticator) ListenerMiddleware(ls ListenerStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return a.withSubject(func(w http.ResponseWriter, r *http.Request, subject string, tc *TokenClaims) {
			a.authorizeListener(w, r, next, ls, subject, tc)
		})
	}
}

// authorizeListener serves next if the verified token's user is an admin
// or an invited listener.
func (a *Authenticator) authorizeListener(w http.ResponseWriter, r *http.Request, next http.Handler, ls ListenerStore,
	subject string, tc *TokenClaims) {
	// No onForbidden: listener refusals are not audited.
	u, ok := a.resolve(w, r, subject, tc, nil)
	if !ok {
		return
	}
	l := Listener{Email: u.Email, Name: u.Name, ImageURL: u.ImageURL, Admin: true}
	if !a.allowed(u) {
		row, ok := a.listenerRow(w, r, ls, u)
		if !ok {
			return
		}
		l.ID, l.Admin = row.ID, false
		if l.Email == "" {
			l.Email = row.Email
		}
	}
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), listenerCtxKey{}, l)))
}

// listenerRow finds the user's live row, accepts an invitation on first
// use and records the visit. It writes the error response and returns false
// when the user may not listen.
func (a *Authenticator) listenerRow(w http.ResponseWriter, r *http.Request, ls ListenerStore, u User) (store.Listener, bool) {
	ctx := r.Context()
	notInvited := func() (store.Listener, bool) {
		a.log.InfoContext(ctx, "listen along refused: not invited", slog.String("user_id", u.ID))
		writeError(w, http.StatusForbidden, "forbidden", "this account is not invited to listen along")
		return store.Listener{}, false
	}
	row, err := ls.ListenerForUser(ctx, u.ID, u.Email)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return notInvited()
	case err != nil:
		a.log.ErrorContext(ctx, "listener lookup failed", logging.Err(err))
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return store.Listener{}, false
	}
	touched := false
	if row.Status == store.ListenerInvited {
		cur, accepted, err := ls.AcceptListener(ctx, row.ID, u.ID)
		if err != nil {
			a.log.ErrorContext(ctx, "listener accept failed", logging.Err(err))
			writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			return store.Listener{}, false
		}
		if accepted {
			touched = true
			if err := ls.Audit(ctx, cur.Email, "listener.accepted",
				map[string]any{"id": cur.ID, "email": cur.Email, "clerk_user_id": u.ID}, clientIP(r)); err != nil {
				a.log.WarnContext(ctx, "audit write failed", logging.Err(err))
			}
		}
		row = cur
	}
	// A concurrent request may have accepted it first; anything else (revoked
	// meanwhile, or bound to another account) is refused.
	if row.Status != store.ListenerActive || row.ClerkUserID == nil || *row.ClerkUserID != u.ID {
		return notInvited()
	}
	if !touched && (row.LastSeenAt == nil || time.Since(*row.LastSeenAt) > listenerTouchEvery) {
		if err := ls.TouchListener(ctx, row.ID); err != nil {
			a.log.WarnContext(ctx, "listener touch failed", logging.Err(err))
		}
	}
	return row, true
}

// ListenerMe serves GET /api/v1/listen/me: the signed-in listener.
func ListenerMe(w http.ResponseWriter, r *http.Request) {
	l, ok := ListenerFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]Listener{"data": l})
}
