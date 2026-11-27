package admin

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// fakeListeners is an in-memory listeners table with the store's rules.
type fakeListeners struct {
	mu      sync.Mutex
	rows    []store.Listener
	next    int64
	err     error // returned by every call when set
	touches int
	audit   fakeAuditor
	// beforeAccept runs inside AcceptListener before the update (races).
	beforeAccept func()
}

func (f *fakeListeners) live(match func(store.Listener) bool) (int, bool) {
	for i, l := range f.rows {
		if l.Status != store.ListenerRevoked && match(l) {
			return i, true
		}
	}
	return 0, false
}

func (f *fakeListeners) ListListeners(context.Context) ([]store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Listener{}, f.rows...), f.err
}

func (f *fakeListeners) GetListener(_ context.Context, id int64) (store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.get(id)
}

func (f *fakeListeners) get(id int64) (store.Listener, error) {
	if f.err != nil {
		return store.Listener{}, f.err
	}
	for _, l := range f.rows {
		if l.ID == id {
			return l, nil
		}
	}
	return store.Listener{}, domain.ErrNotFound
}

func (f *fakeListeners) CreateListener(_ context.Context, n store.NewListener) (store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Listener{}, f.err
	}
	if _, dup := f.live(func(l store.Listener) bool {
		return l.Email == n.Email || (n.ClerkUserID != nil && l.ClerkUserID != nil && *l.ClerkUserID == *n.ClerkUserID)
	}); dup {
		return store.Listener{}, store.ErrListenerExists
	}
	f.next++
	l := store.Listener{ID: f.next, Email: n.Email, Status: store.ListenerInvited, ClerkUserID: n.ClerkUserID,
		ClerkInvitationID: n.ClerkInvitationID, InvitedBy: n.InvitedBy, InvitedAt: time.Now()}
	f.rows = append(f.rows, l)
	return l, nil
}

func (f *fakeListeners) SetListenerInvitation(_ context.Context, id int64, inv string) (store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, l := range f.rows {
		if l.ID == id && l.Status == store.ListenerInvited {
			f.rows[i].ClerkInvitationID = &inv
			return f.rows[i], nil
		}
	}
	return store.Listener{}, domain.ErrNotFound
}

func (f *fakeListeners) RevokeListener(_ context.Context, id int64) (store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.get(id)
	if err != nil {
		return l, err
	}
	if l.Status == store.ListenerRevoked {
		return store.Listener{}, store.ErrListenerRevoked
	}
	for i := range f.rows {
		if f.rows[i].ID == id {
			now := time.Now()
			f.rows[i].Status, f.rows[i].RevokedAt = store.ListenerRevoked, &now
			return f.rows[i], nil
		}
	}
	return store.Listener{}, domain.ErrNotFound
}

func (f *fakeListeners) ListenerForUser(_ context.Context, userID, email string) (store.Listener, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Listener{}, f.err
	}
	email = strings.ToLower(email)
	if i, ok := f.live(func(l store.Listener) bool { return userID != "" && l.ClerkUserID != nil && *l.ClerkUserID == userID }); ok {
		return f.rows[i], nil
	}
	if i, ok := f.live(func(l store.Listener) bool {
		return email != "" && l.Email == email && (userID == "" || l.ClerkUserID == nil)
	}); ok {
		return f.rows[i], nil
	}
	return store.Listener{}, domain.ErrNotFound
}

func (f *fakeListeners) AcceptListener(_ context.Context, id int64, userID string) (store.Listener, bool, error) {
	if f.beforeAccept != nil {
		f.beforeAccept()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, l := range f.rows {
		if l.ID == id && l.Status == store.ListenerInvited && (l.ClerkUserID == nil || *l.ClerkUserID == userID) {
			now := time.Now()
			f.rows[i].Status, f.rows[i].ClerkUserID, f.rows[i].AcceptedAt, f.rows[i].LastSeenAt = store.ListenerActive, &userID, &now, &now
			return f.rows[i], true, nil
		}
	}
	l, err := f.get(id)
	return l, false, err
}

func (f *fakeListeners) TouchListener(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches++
	for i := range f.rows {
		if f.rows[i].ID == id {
			now := time.Now()
			f.rows[i].LastSeenAt = &now
		}
	}
	return nil
}

func (f *fakeListeners) Audit(ctx context.Context, actor, action string, detail any, ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.audit.Audit(ctx, actor, action, detail, ip)
}

// fakeClerk records listener Clerk calls.
type fakeClerk struct {
	users     map[string]string // email → user ID
	roles     map[string]string // user ID → role
	inviteErr error
	findErr   error
	revokeErr error
	roleErr   error
	invites   []string
	revoked   []string
	setRoles  []string
	n         int
}

func (c *fakeClerk) Invite(_ context.Context, email, redirect string) (string, error) {
	if c.inviteErr != nil {
		return "", c.inviteErr
	}
	c.n++
	c.invites = append(c.invites, email+" "+redirect)
	return "inv_" + string(rune('0'+c.n)), nil
}

func (c *fakeClerk) RevokeInvitation(_ context.Context, id string) error {
	c.revoked = append(c.revoked, id)
	return c.revokeErr
}

func (c *fakeClerk) FindUser(_ context.Context, email string) (string, string, bool, error) {
	if c.findErr != nil {
		return "", "", false, c.findErr
	}
	id, ok := c.users[email]
	return id, c.roles[id], ok, nil
}

func (c *fakeClerk) UserRole(_ context.Context, id string) (string, bool, error) {
	if c.roleErr != nil {
		return "", false, c.roleErr
	}
	role, ok := c.roles[id]
	return role, ok, nil
}

func (c *fakeClerk) SetRole(_ context.Context, id string, role *string) error {
	if c.roleErr != nil {
		return c.roleErr
	}
	v := "<nil>"
	if role != nil {
		v = *role
	}
	c.setRoles = append(c.setRoles, id+"="+v)
	return nil
}
