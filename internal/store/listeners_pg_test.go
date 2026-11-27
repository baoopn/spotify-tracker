package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

func ptr(s string) *string { return &s }

func TestPGListeners(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `TRUNCATE listeners CASCADE`)

	a, err := st.CreateListener(ctx, store.NewListener{Email: " Ann@Example.com ", InvitedBy: "admin@x", ClerkInvitationID: ptr("inv_1")})
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "ann@example.com" || a.Status != store.ListenerInvited || a.ClerkUserID != nil {
		t.Errorf("created = %+v", a)
	}
	if _, err := st.CreateListener(ctx, store.NewListener{Email: "ann@example.com", InvitedBy: "x"}); !errors.Is(err, store.ErrListenerExists) {
		t.Errorf("duplicate email err = %v", err)
	}

	// Duplicate check (no user ID) and sign-in lookup by email.
	if l, err := st.ListenerForUser(ctx, "", "ANN@example.com"); err != nil || l.ID != a.ID {
		t.Errorf("by email = %+v, %v", l, err)
	}
	if l, err := st.ListenerForUser(ctx, "user_a", "ann@example.com"); err != nil || l.ID != a.ID {
		t.Errorf("by user+email = %+v, %v", l, err)
	}
	if _, err := st.ListenerForUser(ctx, "user_a", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("by unbound id err = %v", err)
	}

	if l, err := st.SetListenerInvitation(ctx, a.ID, "inv_2"); err != nil || *l.ClerkInvitationID != "inv_2" {
		t.Errorf("set invitation = %+v, %v", l, err)
	}

	// Accept once; the second call reports accepted=false.
	l, ok, err := st.AcceptListener(ctx, a.ID, "user_a")
	if err != nil || !ok || l.Status != store.ListenerActive || l.AcceptedAt == nil || l.LastSeenAt == nil {
		t.Fatalf("accept = %+v %v %v", l, ok, err)
	}
	if _, ok, err := st.AcceptListener(ctx, a.ID, "user_a"); err != nil || ok {
		t.Errorf("second accept = %v %v", ok, err)
	}
	if _, err := st.SetListenerInvitation(ctx, a.ID, "inv_3"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("set invitation on active err = %v", err)
	}
	// Bound row: another Clerk user with the same email does not match it.
	if _, err := st.ListenerForUser(ctx, "user_other", "ann@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("other user err = %v", err)
	}
	if l, err := st.ListenerForUser(ctx, "user_a", ""); err != nil || l.ID != a.ID {
		t.Errorf("by id = %+v, %v", l, err)
	}

	// Touch is throttled to once per 5 minutes.
	exec(t, pool, `UPDATE listeners SET last_seen_at = now() - interval '10 minutes' WHERE id = $1`, a.ID)
	if err := st.TouchListener(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	var recent bool
	if err := pool.QueryRow(ctx, `SELECT last_seen_at > now() - interval '1 minute' FROM listeners WHERE id = $1`, a.ID).Scan(&recent); err != nil || !recent {
		t.Errorf("touch did not update: %v %v", recent, err)
	}

	b, err := st.CreateListener(ctx, store.NewListener{Email: "bob@example.com", InvitedBy: "admin@x", ClerkUserID: ptr("user_b")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateListener(ctx, store.NewListener{Email: "bob2@example.com", InvitedBy: "x", ClerkUserID: ptr("user_b")}); !errors.Is(err, store.ErrListenerExists) {
		t.Errorf("duplicate user err = %v", err)
	}
	// A row created with a user ID can be accepted by that user only.
	if _, ok, _ := st.AcceptListener(ctx, b.ID, "user_x"); ok {
		t.Error("accepted by the wrong user")
	}

	// Revoke: once, then already revoked; unknown id is not found.
	if r, err := st.RevokeListener(ctx, a.ID); err != nil || r.Status != store.ListenerRevoked || r.RevokedAt == nil {
		t.Errorf("revoke = %+v, %v", r, err)
	}
	if _, err := st.RevokeListener(ctx, a.ID); !errors.Is(err, store.ErrListenerRevoked) {
		t.Errorf("second revoke err = %v", err)
	}
	if _, err := st.RevokeListener(ctx, 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown revoke err = %v", err)
	}
	if _, err := st.ListenerForUser(ctx, "user_a", "ann@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("revoked still found: %v", err)
	}
	// Inviting a revoked email again makes a new row.
	c, err := st.CreateListener(ctx, store.NewListener{Email: "ann@example.com", InvitedBy: "admin@x"})
	if err != nil || c.ID == a.ID {
		t.Fatalf("re-invite = %+v, %v", c, err)
	}

	all, err := st.ListListeners(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Order: (none active), invited newest first, then revoked.
	want := []int64{c.ID, b.ID, a.ID}
	if len(all) != len(want) {
		t.Fatalf("list = %+v", all)
	}
	for i, id := range want {
		if all[i].ID != id {
			t.Errorf("list[%d] = %d, want %d", i, all[i].ID, id)
		}
	}
	if _, err := st.GetListener(ctx, 999999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get unknown err = %v", err)
	}
}
