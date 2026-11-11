package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/settings"
)

// accountsServer fakes Spotify's token endpoint. reply decides the answer for
// each refresh token sent; sent records them in order.
type accountsServer struct {
	mu    sync.Mutex
	sent  []string
	reply func(refresh string) (status int, body map[string]any)
}

func (a *accountsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	rt := r.PostForm.Get("refresh_token")
	a.mu.Lock()
	a.sent = append(a.sent, rt)
	a.mu.Unlock()
	status, body := a.reply(rt)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *accountsServer) refreshes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.sent)
}

// logRecorder keeps the messages logged through it.
type logRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecorder) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, r.Message)
	return nil
}
func (l *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logRecorder) WithGroup(string) slog.Handler      { return l }
func (l *logRecorder) has(msg string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.msgs, msg)
}

// fakeStore is an in-memory settings row; failSaves makes the next n saves fail.
type fakeStore struct {
	mu        sync.Mutex
	s         settings.Spotify
	failSaves int
	saves     int
	saveErrs  []error // ctx.Err() seen by each save
}

func (f *fakeStore) load(context.Context) (settings.Spotify, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s, nil
}

func (f *fakeStore) save(ctx context.Context, _ string, s settings.Spotify) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	f.saveErrs = append(f.saveErrs, ctx.Err())
	if f.failSaves > 0 {
		f.failSaves--
		return errors.New("database unavailable")
	}
	f.s = s
	return nil
}

func (f *fakeStore) stored() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s.RefreshToken
}

// newTestManager wires a Manager to a fake accounts server, a fake store and
// an unreachable Redis (so the access-token cache always misses).
func newTestManager(t *testing.T, acc *accountsServer, st *fakeStore, log *logRecorder) *Manager {
	t.Helper()
	srv := httptest.NewServer(acc)
	t.Cleanup(srv.Close)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	h := slog.DiscardHandler
	if log != nil {
		h = log
	}
	m := NewManager(&OAuth{ClientID: "id", ClientSecret: "secret", AccountsBase: srv.URL}, nil, rdb, slog.New(h))
	m.load, m.save = st.load, st.save
	return m
}

func fastSaveBackoff(t *testing.T) {
	t.Helper()
	old := saveBackoff
	saveBackoff = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { saveBackoff = old })
}

func tokenBody(refresh string) map[string]any {
	b := map[string]any{"access_token": "access-" + refresh, "expires_in": 3600}
	if refresh != "" {
		b["refresh_token"] = refresh
	}
	return b
}

func TestAccessTokenInvalidGrantAlertOutsideLock(t *testing.T) {
	acc := &accountsServer{reply: func(string) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant"}
	}}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}}
	m := newTestManager(t, acc, st, nil)

	started := make(chan context.Context, 4)
	release := make(chan struct{})
	done := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	m.OnInvalidGrant = func(ctx context.Context, _ error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		started <- ctx
		<-release
		close(done)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if _, _, err := m.AccessToken(ctx, true); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("first call: err = %v, want ErrInvalidGrant", err)
	}
	cancel()

	var actx context.Context
	select {
	case actx = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("OnInvalidGrant was not called")
	}

	// The callback is still blocked: a second call must not wait for it.
	second := make(chan error, 1)
	go func() {
		_, _, err := m.AccessToken(context.Background(), true)
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, ErrInvalidGrant) {
			t.Fatalf("second call: err = %v, want ErrInvalidGrant", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second AccessToken blocked behind the alert callback")
	}

	if err := actx.Err(); err != nil {
		t.Fatalf("alert context ended with the caller's: %v", err)
	}
	if _, ok := actx.Deadline(); !ok {
		t.Error("alert context has no deadline")
	}
	close(release)
	<-done
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 1 {
		t.Errorf("OnInvalidGrant called %d times while in flight, want 1", calls)
	}
}

func TestAccessTokenRotatedSaveRetries(t *testing.T) {
	fastSaveBackoff(t)
	acc := &accountsServer{reply: func(string) (int, map[string]any) { return http.StatusOK, tokenBody("r2") }}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}, failSaves: 2}
	m := newTestManager(t, acc, st, nil)

	ctx, cancel := context.WithCancel(context.Background())
	save := m.save
	m.save = func(sctx context.Context, actor string, s settings.Spotify) error {
		cancel() // the caller goes away while the token is being saved
		return save(sctx, actor, s)
	}

	tok, _, err := m.AccessToken(ctx, true)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "access-r2" {
		t.Errorf("token = %q, want access-r2", tok)
	}
	if st.saves != 3 {
		t.Errorf("saves = %d, want 3", st.saves)
	}
	for i, e := range st.saveErrs {
		if e != nil {
			t.Errorf("save %d saw a cancelled context: %v", i+1, e)
		}
	}
	if got := st.stored(); got != "r2" {
		t.Errorf("stored refresh token = %q, want r2", got)
	}
	if m.unsaved != "" {
		t.Error("unsaved token kept after a successful save")
	}
}

func TestAccessTokenRotatedSaveFails(t *testing.T) {
	fastSaveBackoff(t)
	acc := &accountsServer{reply: func(rt string) (int, map[string]any) {
		if rt == "r1" {
			return http.StatusOK, tokenBody("r2") // rotate once
		}
		return http.StatusOK, tokenBody("")
	}}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}, failSaves: saveAttempts}
	logs := &logRecorder{}
	m := newTestManager(t, acc, st, logs)

	tok, _, err := m.AccessToken(context.Background(), true)
	if !errors.Is(err, ErrTokenNotSaved) {
		t.Fatalf("err = %v, want ErrTokenNotSaved", err)
	}
	if tok != "" {
		t.Errorf("token = %q, want none", tok)
	}
	if logs.has("cache spotify access token failed") {
		t.Error("access token was cached although the rotated token was not saved")
	}
	if st.saves != saveAttempts {
		t.Errorf("saves = %d, want %d", st.saves, saveAttempts)
	}

	// The store works again: the next refresh sends the unsaved token and saves it.
	tok, _, err = m.AccessToken(context.Background(), true)
	if err != nil {
		t.Fatalf("second AccessToken: %v", err)
	}
	if tok != "access-" {
		t.Errorf("second token = %q", tok)
	}
	if got := acc.refreshes(); !slices.Equal(got, []string{"r1", "r2"}) {
		t.Errorf("refresh tokens sent = %v, want [r1 r2]", got)
	}
	if got := st.stored(); got != "r2" {
		t.Errorf("stored refresh token = %q, want r2", got)
	}
	if m.unsaved != "" {
		t.Error("unsaved token kept after a successful save")
	}
}

func TestAccessTokenUnrotatedSaveFailureOnlyLogged(t *testing.T) {
	fastSaveBackoff(t)
	acc := &accountsServer{reply: func(string) (int, map[string]any) { return http.StatusOK, tokenBody("") }}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}, failSaves: saveAttempts}
	logs := &logRecorder{}
	m := newTestManager(t, acc, st, logs)

	tok, _, err := m.AccessToken(context.Background(), true)
	if err != nil || tok == "" {
		t.Fatalf("AccessToken = %q, %v; want a token", tok, err)
	}
	if !logs.has("save spotify token state failed") {
		t.Error("failed save was not logged")
	}
	if m.unsaved != "" {
		t.Error("unrotated token marked unsaved")
	}
}

func TestConnectClearsUnsaved(t *testing.T) {
	acc := &accountsServer{reply: func(string) (int, map[string]any) { return http.StatusOK, tokenBody("c1") }}
	st := &fakeStore{s: settings.Spotify{RefreshToken: "r1"}}
	m := newTestManager(t, acc, st, nil)
	m.oauth.APIBase = m.oauth.AccountsBase // keep /me on the fake server
	m.unsaved = "r2"

	if _, err := m.Connect(context.Background(), "admin", "code", "http://127.0.0.1/callback"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if m.unsaved != "" {
		t.Errorf("unsaved = %q after Connect, want empty", m.unsaved)
	}
	if got := st.stored(); got != "c1" {
		t.Errorf("stored refresh token = %q, want c1", got)
	}
}
