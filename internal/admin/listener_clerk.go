package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/invitation"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// listenerClerk is the part of Clerk's Backend API the listener admin
// routes use (tests replace it).
type listenerClerk interface {
	// Invite sends a Clerk invitation email with the listener role in
	// public_metadata; the link lands on redirectURL.
	Invite(ctx context.Context, email, redirectURL string) (invitationID string, err error)
	// RevokeInvitation revokes a pending invitation. An invitation that is
	// already accepted or revoked is not an error.
	RevokeInvitation(ctx context.Context, id string) error
	// FindUser finds the Clerk user with this email address.
	FindUser(ctx context.Context, email string) (id, role string, found bool, err error)
	// UserRole reads a user's public_metadata.role (found is false when
	// Clerk no longer has the user).
	UserRole(ctx context.Context, userID string) (role string, found bool, err error)
	// SetRole sets public_metadata.role; nil deletes it.
	SetRole(ctx context.Context, userID string, role *string) error
}

// sdkListenerClerk calls Clerk through the SDK (the key is set by
// NewAuthenticator).
type sdkListenerClerk struct{}

func (sdkListenerClerk) Invite(ctx context.Context, email, redirectURL string) (string, error) {
	meta := json.RawMessage(`{"role":"` + ListenerRole + `"}`)
	notify, ignore := true, true
	params := &invitation.CreateParams{EmailAddress: email, PublicMetadata: &meta, Notify: &notify,
		IgnoreExisting: &ignore}
	if redirectURL != "" {
		params.RedirectURL = &redirectURL
	}
	inv, err := invitation.Create(ctx, params)
	if err != nil {
		return "", err
	}
	return inv.ID, nil
}

func (sdkListenerClerk) RevokeInvitation(ctx context.Context, id string) error {
	_, err := invitation.Revoke(ctx, id)
	switch clerkStatus(err) {
	case http.StatusNotFound, http.StatusBadRequest, http.StatusUnprocessableEntity:
		return nil // already accepted, revoked or gone
	}
	return err
}

func (sdkListenerClerk) FindUser(ctx context.Context, email string) (string, string, bool, error) {
	list, err := user.List(ctx, &user.ListParams{EmailAddresses: []string{email}})
	if err != nil {
		return "", "", false, err
	}
	for _, u := range list.Users {
		if u != nil {
			return u.ID, metadataRole(u.PublicMetadata), true, nil
		}
	}
	return "", "", false, nil
}

func (sdkListenerClerk) UserRole(ctx context.Context, userID string) (string, bool, error) {
	u, err := user.Get(ctx, userID)
	if clerkStatus(err) == http.StatusNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return metadataRole(u.PublicMetadata), true, nil
}

func (sdkListenerClerk) SetRole(ctx context.Context, userID string, role *string) error {
	raw, err := json.Marshal(map[string]*string{"role": role})
	if err != nil {
		return err
	}
	meta := json.RawMessage(raw)
	_, err = user.UpdateMetadata(ctx, userID, &user.UpdateMetadataParams{PublicMetadata: &meta})
	return err
}

// clerkRejected reports whether Clerk refused the request (a 4xx other
// than 429), and its first message.
func clerkRejected(err error) (string, bool) {
	var apiErr *clerk.APIErrorResponse
	if !errors.As(err, &apiErr) || apiErr.HTTPStatusCode < 400 || apiErr.HTTPStatusCode >= 500 ||
		apiErr.HTTPStatusCode == http.StatusTooManyRequests {
		return "", false
	}
	for _, e := range apiErr.Errors {
		if e.LongMessage != "" {
			return e.LongMessage, true
		}
		if e.Message != "" {
			return e.Message, true
		}
	}
	return "Clerk refused the request", true
}
