package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/listen"
	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// ListenInfo describes listen along for the Listeners page.
type ListenInfo struct {
	// Enabled mirrors LISTEN_ENABLED.
	Enabled bool
	// AudioConfigured reports whether the S3 bucket is configured.
	AudioConfigured bool
	// URL is PUBLIC_URL + "/listen", where invitations land.
	URL string
}

// listenerAdminStore manages listener rows (implemented by *store.Store).
type listenerAdminStore interface {
	ListListeners(ctx context.Context) ([]store.Listener, error)
	GetListener(ctx context.Context, id int64) (store.Listener, error)
	CreateListener(ctx context.Context, n store.NewListener) (store.Listener, error)
	SetListenerInvitation(ctx context.Context, id int64, invitationID string) (store.Listener, error)
	RevokeListener(ctx context.Context, id int64) (store.Listener, error)
	ListenerForUser(ctx context.Context, clerkUserID, email string) (store.Listener, error)
	ListenerHistory(ctx context.Context, listenerID int64, limit int, cursor string) ([]store.ListenPlay, *string, error)
}

// mountListeners registers the listener routes. They are writes or reveal
// email addresses, so they are never part of ReadOnlyHandler.
func (a *API) mountListeners(r chi.Router) {
	r.Get("/listeners", a.listListeners)
	r.Get("/listeners/live", a.listenersLive)
	a.mountListenRequests(r)
	r.Post("/listeners", a.inviteListener)
	r.Post("/listeners/{id}/resend", a.resendListener)
	r.Post("/listeners/{id}/revoke", a.revokeListener)
	r.Get("/listeners/{id}/history", a.listenerHistory)
}

// listenerHistory serves a listener's counted plays (any status), newest
// first. Read-only and not audited.
func (a *API) listenerHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := listenerID(w, r)
	if !ok {
		return
	}
	limit, err := intParam(r, "limit", 50, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	db := a.listenerDB()
	if _, err := db.GetListener(r.Context(), id); errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "listener not found")
		return
	} else if err != nil {
		a.internal(w, r, err)
		return
	}
	plays, next, err := db.ListenerHistory(r.Context(), id, limit, r.URL.Query().Get("cursor"))
	if errors.Is(err, store.ErrBadCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "invalid cursor")
		return
	} else if err != nil {
		a.internal(w, r, err)
		return
	}
	if plays == nil {
		plays = []store.ListenPlay{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": plays, "next_cursor": next})
}

func (a *API) listenerDB() listenerAdminStore {
	if a.listeners != nil {
		return a.listeners
	}
	return a.Store
}

func (a *API) listenerClerkAPI() listenerClerk {
	if a.clerkInv != nil {
		return a.clerkInv
	}
	return sdkListenerClerk{}
}

type listenerView struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	Status      string     `json:"status"`
	ClerkUserID *string    `json:"clerk_user_id"`
	HasAccount  bool       `json:"has_account"`
	InvitedBy   string     `json:"invited_by"`
	InvitedAt   time.Time  `json:"invited_at"`
	AcceptedAt  *time.Time `json:"accepted_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

func toListenerView(l store.Listener) listenerView {
	return listenerView{ID: l.ID, Email: l.Email, Status: l.Status, ClerkUserID: l.ClerkUserID,
		HasAccount: l.ClerkUserID != nil, InvitedBy: l.InvitedBy, InvitedAt: l.InvitedAt.UTC(),
		AcceptedAt: optUTC(l.AcceptedAt), RevokedAt: optUTC(l.RevokedAt), LastSeenAt: optUTC(l.LastSeenAt)}
}

func (a *API) listListeners(w http.ResponseWriter, r *http.Request) {
	rows, err := a.listenerDB().ListListeners(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	out := make([]listenerView, len(rows))
	for i, l := range rows {
		out[i] = toListenerView(l)
	}
	pending, err := a.requestDB().PendingListenRequests(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out, "listen_enabled": a.Listen.Enabled,
		"audio_configured": a.Listen.AudioConfigured, "listen_url": a.Listen.URL, "pending_requests": pending})
}

// validEmail normalizes an email address (trimmed, lower case) and reports
// whether it is a bare address (the rule "Request an invite" uses too).
func validEmail(raw string) (string, bool) { return listen.NormalizeEmail(raw) }

// clerkFailed writes the response for a failed Clerk call.
func (a *API) clerkFailed(w http.ResponseWriter, r *http.Request, err error) {
	if msg, ok := clerkRejected(err); ok {
		a.Log.WarnContext(r.Context(), "clerk refused a listener request", logging.Err(err))
		writeError(w, http.StatusConflict, "clerk_rejected", msg)
		return
	}
	a.Log.ErrorContext(r.Context(), "clerk listener request failed", logging.Err(err))
	writeError(w, http.StatusServiceUnavailable, "clerk_unavailable", "could not reach Clerk; try again")
}

func (a *API) inviteListener(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body: "+err.Error())
		return
	}
	email, ok := validEmail(in.Email)
	if !ok {
		writeValidation(w, map[string]string{"email": "must be a valid email address"})
		return
	}
	l, found, ok := a.invite(w, r, email)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": toListenerView(l), "invitation_sent": !found})
}

// invite invites a validated email: an existing Clerk account gets the
// listener role, anyone else a Clerk invitation; then the row is created
// and audited. found reports an existing account. On failure it writes the
// error response and returns ok false. "Request an invite" uses it too.
func (a *API) invite(w http.ResponseWriter, r *http.Request, email string) (l store.Listener, found, ok bool) {
	ctx := r.Context()
	db, cl := a.listenerDB(), a.listenerClerkAPI()
	if _, err := db.ListenerForUser(ctx, "", email); err == nil {
		writeError(w, http.StatusConflict, "listener_exists", "this email is already invited")
		return l, false, false
	} else if !errors.Is(err, domain.ErrNotFound) {
		a.internal(w, r, err)
		return l, false, false
	}
	userID, role, found, err := cl.FindUser(ctx, email)
	if err != nil {
		a.clerkFailed(w, r, err)
		return l, false, false
	}
	n := store.NewListener{Email: email}
	if u, ok := UserFrom(ctx); ok {
		n.InvitedBy = u.Email
	}
	roleSet := false
	if found {
		// An existing account signs in at /listen; no invitation email.
		// Its role is set only when it has none (an admin keeps theirs).
		if role == "" {
			lr := ListenerRole
			if err := cl.SetRole(ctx, userID, &lr); err != nil {
				a.clerkFailed(w, r, err)
				return l, false, false
			}
			roleSet = true
		}
		n.ClerkUserID = &userID
	} else {
		invID, err := cl.Invite(ctx, email, a.Listen.URL)
		if err != nil {
			a.clerkFailed(w, r, err)
			return l, false, false
		}
		n.ClerkInvitationID = &invID
	}
	l, err = db.CreateListener(ctx, n)
	if err != nil {
		if n.ClerkInvitationID != nil {
			if rerr := cl.RevokeInvitation(ctx, *n.ClerkInvitationID); rerr != nil {
				a.Log.WarnContext(ctx, "revoking an unused listener invitation failed", logging.Err(rerr))
			}
		}
		if roleSet {
			// Undo only the role this call set.
			if rerr := cl.SetRole(ctx, userID, nil); rerr != nil {
				a.Log.WarnContext(ctx, "removing an unused listener role failed", logging.Err(rerr))
			}
		}
		if errors.Is(err, store.ErrListenerExists) {
			writeError(w, http.StatusConflict, "listener_exists", "this email is already invited")
			return l, false, false
		}
		a.internal(w, r, err)
		return l, false, false
	}
	a.audit(r, "listener.invited", map[string]any{"id": l.ID, "email": l.Email, "existing_account": found})
	return l, found, true
}

// listenerID parses {id} (a positive int64); false answers 404.
func listenerID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "listener not found")
		return 0, false
	}
	return id, true
}

func (a *API) resendListener(w http.ResponseWriter, r *http.Request) {
	id, ok := listenerID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	db, cl := a.listenerDB(), a.listenerClerkAPI()
	l, err := db.GetListener(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "listener not found")
		return
	} else if err != nil {
		a.internal(w, r, err)
		return
	}
	if l.Status != store.ListenerInvited {
		writeError(w, http.StatusConflict, "not_pending", "this invitation is no longer pending")
		return
	}
	if l.ClerkUserID != nil {
		writeError(w, http.StatusConflict, "no_invitation",
			"This person already has an account. Ask them to sign in at /listen.")
		return
	}
	// Clerk has no resend: revoke the old invitation, then send a new one.
	if l.ClerkInvitationID != nil {
		if err := cl.RevokeInvitation(ctx, *l.ClerkInvitationID); err != nil {
			a.Log.WarnContext(ctx, "revoking the old listener invitation failed", logging.Err(err))
		}
	}
	invID, err := cl.Invite(ctx, l.Email, a.Listen.URL)
	if err != nil {
		a.clerkFailed(w, r, err)
		return
	}
	l, err = db.SetListenerInvitation(ctx, id, invID)
	if err != nil {
		if rerr := cl.RevokeInvitation(ctx, invID); rerr != nil {
			a.Log.WarnContext(ctx, "revoking an unused listener invitation failed", logging.Err(rerr))
		}
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusConflict, "not_pending", "this invitation is no longer pending")
			return
		}
		a.internal(w, r, err)
		return
	}
	a.audit(r, "listener.resent", map[string]any{"id": l.ID, "email": l.Email})
	writeJSON(w, http.StatusOK, map[string]any{"data": toListenerView(l)})
}

// Clerk cleanup results of a revoke.
const (
	cleanupOK      = "ok"
	cleanupFailed  = "failed"
	cleanupSkipped = "skipped"
)

func (a *API) revokeListener(w http.ResponseWriter, r *http.Request) {
	id, ok := listenerID(w, r)
	if !ok {
		return
	}
	// The row first: access ends here, whatever Clerk does next.
	l, err := a.listenerDB().RevokeListener(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "listener not found")
		return
	case errors.Is(err, store.ErrListenerRevoked):
		writeError(w, http.StatusConflict, "already_revoked", "this listener is already revoked")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	a.dropPresence(r.Context(), l.ID)
	cleanup := a.cleanupListener(r.Context(), l)
	a.audit(r, "listener.revoked", map[string]any{"id": l.ID, "email": l.Email, "clerk_cleanup": cleanup})
	writeJSON(w, http.StatusOK, map[string]any{"data": toListenerView(l), "clerk_cleanup": cleanup})
}

// cleanupListener revokes a pending invitation and removes the listener
// role from the Clerk user (never another role). Failures are logged; the
// row is revoked either way.
func (a *API) cleanupListener(ctx context.Context, l store.Listener) string {
	cl := a.listenerClerkAPI()
	result := cleanupSkipped
	fail := func(msg string, err error) {
		a.Log.WarnContext(ctx, msg, logging.Err(err))
		result = cleanupFailed
	}
	if l.ClerkInvitationID != nil && l.AcceptedAt == nil {
		result = cleanupOK
		if err := cl.RevokeInvitation(ctx, *l.ClerkInvitationID); err != nil {
			fail("revoking the listener invitation failed", err)
		}
	}
	// The Clerk user: the bound one, or (an invitee who signed up but never
	// opened /listen) the account with the invited email.
	var userID, role string
	var found bool
	var err error
	if l.ClerkUserID != nil {
		userID = *l.ClerkUserID
		role, found, err = cl.UserRole(ctx, userID)
	} else {
		userID, role, found, err = cl.FindUser(ctx, l.Email)
	}
	if l.ClerkUserID != nil || err != nil || found {
		if result == cleanupSkipped {
			result = cleanupOK
		}
	}
	switch {
	case err != nil:
		fail("reading the listener's Clerk role failed", err)
	case found && role == ListenerRole:
		if err := cl.SetRole(ctx, userID, nil); err != nil {
			fail("removing the listener role failed", err)
		}
	}
	if userID != "" && a.Redis != nil {
		_ = a.Redis.Del(ctx, ProfileCacheKey(userID)).Err()
	}
	return result
}
