package spotify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Following a playlist puts it in the user's library; unfollowing it is how
// Spotify deletes a playlist its owner created. The Web API reference
// (checked Nov 2026) lists PUT/DELETE /me/library?uris=spotify:playlist:{id}
// for this and marks PUT/DELETE /playlists/{id}/followers as deprecated. The
// library endpoints are used first; the deprecated ones are the fallback when
// the library endpoint refuses the token (403: it also lists
// user-library-modify and user-follow-modify, which older tokens may lack)
// and the double check before a playlist is reported gone (404).

const (
	ownedPageSize = 50 // Spotify's maximum for GET /me/playlists
	ownedMaxPages = 20
)

func libraryPath(id string) string {
	return "/me/library?uris=" + url.QueryEscape("spotify:playlist:"+id)
}

func followersPath(id string) string { return "/playlists/" + url.PathEscape(id) + "/followers" }

// UnfollowPlaylist removes a playlist from the user's library, which deletes
// a playlist the user owns. A playlist that is already gone (404) is not an
// error.
func (m *Manager) UnfollowPlaylist(ctx context.Context, id string) error {
	err := m.sendJSON(ctx, http.MethodDelete, "remove playlist from library", libraryPath(id), nil, nil)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		// Only the followers endpoint's 404 means the playlist is gone.
		err = m.sendJSON(ctx, http.MethodDelete, "unfollow playlist", followersPath(id), nil, nil)
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// FollowPlaylist puts a playlist back in the user's library (publicly), which
// restores a playlist its owner deleted. It returns a wrapped ErrNotFound
// when the playlist no longer exists at all.
//
// The library endpoint has no visibility option, so after it the playlist
// is made public again with PUT /playlists/{id} {"public": true} (the
// documented way to put a playlist on the user's profile). The deprecated
// followers endpoint takes {"public": true} itself.
func (m *Manager) FollowPlaylist(ctx context.Context, id string) error {
	err := m.sendJSON(ctx, http.MethodPut, "save playlist to library", libraryPath(id), nil, nil)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
		return m.sendJSON(ctx, http.MethodPut, "follow playlist", followersPath(id), map[string]any{"public": true}, nil)
	}
	if err != nil {
		return err
	}
	return m.sendJSON(ctx, http.MethodPut, "make playlist public",
		"/playlists/"+url.PathEscape(id), map[string]any{"public": true}, nil)
}

// OwnedPlaylistIDs returns the IDs of the playlists in the user's library
// (GET /me/playlists, every page). Any failure is an error, including a
// library too large to list in ownedMaxPages pages: a partial list would
// make present playlists look deleted.
func (m *Manager) OwnedPlaylistIDs(ctx context.Context) (map[string]bool, error) {
	ids := map[string]bool{}
	for page := 0; page < ownedMaxPages; page++ {
		var resp struct {
			Items []*struct {
				ID string `json:"id"`
			} `json:"items"`
			Next *string `json:"next"`
		}
		path := "/me/playlists?limit=" + strconv.Itoa(ownedPageSize) + "&offset=" + strconv.Itoa(page*ownedPageSize)
		found, err := m.getJSON(ctx, "my playlists", path, &resp)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: list my playlists", ErrForbidden)
		}
		for _, it := range resp.Items {
			if it != nil && it.ID != "" {
				ids[it.ID] = true
			}
		}
		if resp.Next == nil || *resp.Next == "" || len(resp.Items) == 0 {
			return ids, nil
		}
	}
	return nil, fmt.Errorf("spotify: list my playlists: more than %d playlists", ownedPageSize*ownedMaxPages)
}

// CurrentUserName returns the connected account's display name (GET /me),
// or its user ID when the display name is empty.
func (m *Manager) CurrentUserName(ctx context.Context) (string, error) {
	var p Profile
	found, err := m.getJSON(ctx, "current user", "/me", &p)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("%w: current user", ErrForbidden)
	}
	if p.DisplayName != "" {
		return p.DisplayName, nil
	}
	if p.ID == "" {
		return "", errors.New("spotify: current user has no display name or id")
	}
	return p.ID, nil
}
