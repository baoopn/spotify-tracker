package listen

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/logging"
	"github.com/baoopn/spotify-tracker/internal/notify"
	"github.com/baoopn/spotify-tracker/internal/platform/redisx"
	"github.com/baoopn/spotify-tracker/internal/store"
)

// Limits of "Request an invite".
const (
	RequestNameMax    = 80  // runes
	RequestMessageMax = 500 // runes
	RequestsPerHour   = 3   // per client IP
	MaxPendingRequest = 100
	requestWindow     = time.Hour
	alertTimeout      = 30 * time.Second
)

// ErrRateLimited means the client sent too many requests this hour.
type ErrRateLimited struct{ RetryAfter time.Duration }

func (e ErrRateLimited) Error() string { return "too many listen requests" }

// NormalizeEmail trims and lower-cases an email address and reports whether
// it is a bare address of at most 254 characters (the invite rule).
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) < 3 || len(email) > 254 {
		return email, false
	}
	addr, err := mail.ParseAddress(email)
	return email, err == nil && addr.Name == "" && addr.Address == email
}

// CleanText trims s and drops control characters; keepNewlines keeps line
// breaks (normalized to \n) for multi-line text.
func CleanText(s string, keepNewlines bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && keepNewlines:
			b.WriteRune(r)
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ')
		case unicode.IsControl(r) || r == utf8.RuneError || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// RequestInput is a validated "Request an invite" submission.
type RequestInput struct {
	Name, Email string
	Message     string // "" when none
	Country     string // "" when unknown
	// ClientHash is the salted hash of the client IP (never the IP itself).
	ClientHash string
}

// requestStore stores requests and audits (implemented by *store.Store).
type requestStore interface {
	SubmitListenRequest(ctx context.Context, n store.NewListenRequest, maxPending int) (store.ListenRequest, bool, error)
	Audit(ctx context.Context, actor, action string, detail any, ip string) error
}

// mailer sends alert emails (implemented by *notify.Notifier).
type mailer interface {
	Configured(ctx context.Context) bool
	Send(ctx context.Context, to []string, e notify.Email) error
}

// rateKV counts requests per client and window (Redis; tests replace it).
type rateKV interface {
	incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

type redisRate struct{ rdb *redis.Client }

func (r redisRate) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := r.rdb.TxPipeline()
	n := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return n.Val(), nil
}

// Requests handles "Request an invite": rate limit, storage, audit and the
// alert email.
type Requests struct {
	db     requestStore
	rate   rateKV
	mail   mailer // nil: no alert
	email  func(r store.ListenRequest) notify.Email
	log    *slog.Logger
	now    func() time.Time
	goFunc func(func()) // runs the alert (tests run it inline)
}

// NewRequests builds the request handler. mail may be nil; email builds the
// alert for a new request.
func NewRequests(db requestStore, rdb *redis.Client, mail mailer, email func(store.ListenRequest) notify.Email, log *slog.Logger) *Requests {
	return &Requests{db: db, rate: redisRate{rdb}, mail: mail, email: email, log: log, now: time.Now,
		goFunc: func(f func()) { go f() }}
}

// Submit stores a request. It answers ErrRateLimited after RequestsPerHour
// requests from one client in the current hour. Every other outcome (a new
// request, an update of the pending one, too many pending) returns nil, so
// callers cannot learn anything about the email.
func (q *Requests) Submit(ctx context.Context, in RequestInput) error {
	now := q.now()
	window := now.Truncate(requestWindow)
	key := redisx.KeyListenRequestRatePrefix + in.ClientHash + ":" + fmt.Sprint(window.Unix())
	n, err := q.rate.incr(ctx, key, requestWindow)
	if err != nil {
		return fmt.Errorf("listen request rate limit: %w", err)
	}
	if n > RequestsPerHour {
		return ErrRateLimited{RetryAfter: window.Add(requestWindow).Sub(now)}
	}
	nr := store.NewListenRequest{Name: in.Name, Email: in.Email}
	if in.Message != "" {
		nr.Message = &in.Message
	}
	if in.Country != "" {
		nr.Country = &in.Country
	}
	r, created, err := q.db.SubmitListenRequest(ctx, nr, MaxPendingRequest)
	if errors.Is(err, store.ErrRequestsFull) {
		q.log.WarnContext(ctx, "listen request dropped: too many pending requests", slog.Int("max", MaxPendingRequest))
		return nil
	}
	if err != nil {
		return err
	}
	if !created {
		return nil // an update of the pending request: no second alert
	}
	if err := q.db.Audit(ctx, r.Email, "listener.requested", map[string]any{"id": r.ID}, ""); err != nil {
		q.log.WarnContext(ctx, "audit write failed", logging.Err(err))
	}
	q.alert(ctx, r)
	return nil
}

// alert emails the admin about a new request, without making the request
// wait. Without SMTP settings it does nothing.
func (q *Requests) alert(parent context.Context, r store.ListenRequest) {
	if q.mail == nil || q.email == nil {
		return
	}
	q.goFunc(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), alertTimeout)
		defer cancel()
		if !q.mail.Configured(ctx) {
			return
		}
		if err := q.mail.Send(ctx, nil, q.email(r)); err != nil {
			q.log.WarnContext(ctx, "listen request alert failed", slog.Int64("request_id", r.ID), logging.Err(err))
		}
	})
}
