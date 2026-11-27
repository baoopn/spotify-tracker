package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/store"
)

func TestPGListenRequests(t *testing.T) {
	st, pool := pgStore(t)
	ctx := context.Background()
	exec(t, pool, `TRUNCATE listen_requests, listeners CASCADE`)
	msg, vn := "We met at school", "VN"

	r, created, err := st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "Ann", Email: "ann@example.com", Message: &msg, Country: &vn}, 100)
	if err != nil || !created || r.Status != store.RequestPending || *r.Message != msg || *r.Country != "VN" || r.AlreadyListener {
		t.Fatalf("submit = %+v %v %v", r, created, err)
	}
	// Same email again: not duplicated, and the first name and message stay
	// (anyone may submit any email).
	other := "Not me"
	r2, created, err := st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "Mallory", Email: "ann@example.com", Message: &other}, 100)
	if err != nil || created || r2.ID != r.ID || r2.Name != "Ann" || *r2.Message != msg || *r2.Country != "VN" ||
		!r2.UpdatedAt.After(r.UpdatedAt) {
		t.Fatalf("resubmit = %+v %v %v", r2, created, err)
	}
	// An empty message is filled by a later submission.
	b, _, err := st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "Bea", Email: "bea@example.com"}, 100)
	if err != nil || b.Message != nil {
		t.Fatalf("bea = %+v %v", b, err)
	}
	if b2, _, _ := st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "Bea", Email: "bea@example.com", Message: &other}, 100); b2.Message == nil || *b2.Message != other {
		t.Errorf("fill = %+v", b2)
	}
	exec(t, pool, `DELETE FROM listen_requests WHERE email = 'bea@example.com'`)
	// already_listener follows the listeners table.
	if _, err := st.CreateListener(ctx, store.NewListener{Email: "ann@example.com", InvitedBy: "a"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetListenRequest(ctx, r.ID); !got.AlreadyListener {
		t.Errorf("already_listener = false")
	}

	// Decide: dismiss once, then not pending; unknown is not found.
	d, err := st.DecideListenRequest(ctx, r.ID, store.RequestDismissed, "admin@example.com")
	if err != nil || d.Status != store.RequestDismissed || d.DecidedAt == nil || *d.DecidedBy != "admin@example.com" {
		t.Fatalf("dismiss = %+v %v", d, err)
	}
	if _, err := st.DecideListenRequest(ctx, r.ID, store.RequestInvited, "x"); !errors.Is(err, store.ErrRequestNotPending) {
		t.Errorf("second decide err = %v", err)
	}
	if _, err := st.DecideListenRequest(ctx, 999999, store.RequestInvited, "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown err = %v", err)
	}
	// Dismissed before: a new pending row.
	r3, created, err := st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "Ann", Email: "ann@example.com"}, 100)
	if err != nil || !created || r3.ID == r.ID {
		t.Fatalf("after dismiss = %+v %v %v", r3, created, err)
	}

	// The pending cap, also under concurrent submissions.
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range 6 {
		wg.Go(func() {
			email := string(rune('a'+i)) + "@cap.example.com"
			_, _, errs[i] = st.SubmitListenRequest(ctx, store.NewListenRequest{Name: "X", Email: email}, 4)
		})
	}
	wg.Wait()
	full := 0
	for _, err := range errs {
		if errors.Is(err, store.ErrRequestsFull) {
			full++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := st.PendingListenRequests(ctx); n != 4 || full != 3 {
		t.Errorf("pending = %d, full = %d (want 4 and 3)", n, full)
	}

	rows, pending, err := st.ListListenRequests(ctx, "pending")
	if err != nil || len(rows) != 4 || pending != 4 {
		t.Errorf("pending list = %d %d %v", len(rows), pending, err)
	}
	if rows, _, _ := st.ListListenRequests(ctx, "dismissed"); len(rows) != 1 {
		t.Errorf("dismissed list = %d", len(rows))
	}
	if rows, _, _ := st.ListListenRequests(ctx, "all"); len(rows) != 5 {
		t.Errorf("all list = %d", len(rows))
	}

	// Retention: decided rows older than 90 days go; pending ones stay.
	exec(t, pool, `UPDATE listen_requests SET decided_at = now() - interval '91 days' WHERE status = 'dismissed'`)
	if n, err := st.PruneListenRequests(ctx, 90*24*time.Hour); err != nil || n != 1 {
		t.Errorf("prune = %d %v", n, err)
	}
	if n, _ := st.PendingListenRequests(ctx); n != 4 {
		t.Errorf("prune removed pending rows: %d", n)
	}
}
