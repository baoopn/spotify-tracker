package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// requestAdminStore reads and decides "Request an invite" submissions
// (implemented by *store.Store).
type requestAdminStore interface {
	ListListenRequests(ctx context.Context, status string) ([]store.ListenRequest, int64, error)
	GetListenRequest(ctx context.Context, id int64) (store.ListenRequest, error)
	DecideListenRequest(ctx context.Context, id int64, status, by string) (store.ListenRequest, error)
	PendingListenRequests(ctx context.Context) (int64, error)
}

func (a *API) requestDB() requestAdminStore {
	if a.requests != nil {
		return a.requests
	}
	return a.Store
}

// mountListenRequests registers the request routes (never in
// ReadOnlyHandler).
func (a *API) mountListenRequests(r chi.Router) {
	r.Get("/listeners/requests", a.listRequests)
	r.Post("/listeners/requests/{id}/invite", a.inviteRequest)
	r.Post("/listeners/requests/{id}/dismiss", a.dismissRequest)
}

func (a *API) listRequests(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "":
		status = store.RequestPending
	case store.RequestPending, store.RequestDismissed, "all":
	default:
		writeError(w, http.StatusBadRequest, "invalid_status", "status must be pending, dismissed or all")
		return
	}
	rows, pending, err := a.requestDB().ListListenRequests(r.Context(), status)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows, "pending": pending})
}

// loadRequest loads the request {id}; false has written the error.
func (a *API) loadRequest(w http.ResponseWriter, r *http.Request) (store.ListenRequest, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "not_found", "request not found")
		return store.ListenRequest{}, false
	}
	req, err := a.requestDB().GetListenRequest(r.Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "request not found")
		return req, false
	} else if err != nil {
		a.internal(w, r, err)
		return req, false
	}
	return req, true
}

func notPending(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, "not_pending", "this request was already handled")
}

func actorEmail(r *http.Request) string {
	if u, ok := UserFrom(r.Context()); ok {
		return u.Email
	}
	return ""
}

// inviteRequest invites the requester through the same path as POST
// /listeners (same errors), then marks the request invited.
func (a *API) inviteRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadRequest(w, r)
	if !ok {
		return
	}
	if req.Status != store.RequestPending {
		notPending(w)
		return
	}
	l, found, ok := a.invite(w, r, req.Email)
	if !ok {
		return
	}
	decided, err := a.requestDB().DecideListenRequest(r.Context(), req.ID, store.RequestInvited, actorEmail(r))
	switch {
	case err == nil:
		req = decided
	case errors.Is(err, store.ErrRequestNotPending):
		// Dismissed meanwhile: the invite still stands.
		if cur, gerr := a.requestDB().GetListenRequest(r.Context(), req.ID); gerr == nil {
			req = cur
		}
	default:
		a.internal(w, r, err)
		return
	}
	a.audit(r, "listener.request_invited", map[string]any{"id": req.ID, "email": req.Email})
	writeJSON(w, http.StatusCreated, map[string]any{"data": toListenerView(l), "invitation_sent": !found, "request": req})
}

func (a *API) dismissRequest(w http.ResponseWriter, r *http.Request) {
	req, ok := a.loadRequest(w, r)
	if !ok {
		return
	}
	req, err := a.requestDB().DecideListenRequest(r.Context(), req.ID, store.RequestDismissed, actorEmail(r))
	switch {
	case errors.Is(err, store.ErrRequestNotPending):
		notPending(w)
		return
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "request not found")
		return
	case err != nil:
		a.internal(w, r, err)
		return
	}
	a.audit(r, "listener.request_dismissed", map[string]any{"id": req.ID, "email": req.Email})
	writeJSON(w, http.StatusOK, map[string]any{"request": req})
}
