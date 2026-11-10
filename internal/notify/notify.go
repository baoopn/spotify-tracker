// Package notify sends operator alerts by email (SMTP), de-duplicated in
// Redis so each alert goes out once.
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/baoopn/spotify-tracker/internal/logging"
)

const dedupePrefix = "spotify-tracker:notify:"

// SMTPConfig configures the mailer. Port 465 uses implicit TLS; any other
// port uses STARTTLS when the server offers it.
type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	To       []string
}

// Enabled reports whether enough settings are present to send mail.
func (c SMTPConfig) Enabled() bool { return c.Host != "" && c.From != "" && len(c.To) > 0 }

// ConfigFunc returns the current SMTP configuration (read from settings so
// dashboard edits apply without a restart).
type ConfigFunc func(ctx context.Context) (SMTPConfig, error)

// LogoFunc returns the current header logo (e.g. from the brand API cache)
// and its size in px; ok is false when there is none.
type LogoFunc func() (url string, width, height int, ok bool)

// Notifier sends alerts.
type Notifier struct {
	config ConfigFunc
	rdb    *redis.Client
	brand  Brand
	logo   atomic.Pointer[LogoFunc]
	log    *slog.Logger
}

// New creates a Notifier. If the config is not Enabled, alerts are only
// logged. brand sets the header logo and footer links.
func New(config ConfigFunc, rdb *redis.Client, brand Brand, log *slog.Logger) *Notifier {
	return &Notifier{config: config, rdb: rdb, brand: brand, log: log}
}

// SetLogo sets where the header logo comes from at send time. When f
// reports no logo, the static brand (EMAIL_LOGO_URL or PUBLIC_URL +
// LogoPath) is used.
func (n *Notifier) SetLogo(f LogoFunc) {
	if f == nil {
		n.logo.Store(nil)
		return
	}
	n.logo.Store(&f)
}

// Brand is the brand the notifier's emails use, resolved at call time: the
// SetLogo logo when it has one, else the static brand.
func (n *Notifier) Brand() Brand {
	if f := n.logo.Load(); f != nil {
		if u, w, h, ok := (*f)(); ok {
			return n.brand.WithLogo(u, w, h)
		}
	}
	return n.brand
}

// Send delivers a message immediately to the given recipients (or the
// configured alert recipients when to is empty).
func (n *Notifier) Send(ctx context.Context, to []string, e Email) error {
	cfg, err := n.config(ctx)
	if err != nil {
		return err
	}
	return n.sendTo(cfg, to, e)
}

// SendTest sends the admin dashboard's test email (see TestEmail).
func (n *Notifier) SendTest(ctx context.Context, to []string, sentBy string) error {
	cfg, err := n.config(ctx)
	if err != nil {
		return err
	}
	return n.sendTo(cfg, to, TestEmail(sentBy, cfg.Host, n.Brand(), time.Now()))
}

func (n *Notifier) sendTo(cfg SMTPConfig, to []string, e Email) error {
	if len(to) > 0 {
		cfg.To = to
	}
	if cfg.Host == "" || cfg.From == "" || len(cfg.To) == 0 {
		return errors.New("email is not configured: host, from and at least one recipient are required")
	}
	return n.send(cfg, e)
}

// TestEmail is the "email alerts are working" message sent from Settings →
// Email.
func TestEmail(sentBy, smtpHost string, b Brand, now time.Time) Email {
	e := Email{
		Subject:    "[spotify-tracker] Test email",
		Preheader:  "Your alert email settings work. Nothing else to do.",
		Tone:       Success,
		Badge:      "Test",
		Title:      "Email alerts are working",
		Paragraphs: []string{"This is a test email from the spotify-tracker admin dashboard. Alerts about the Spotify connection will arrive like this one."},
		Facts: []Fact{
			{"Sent by", sentBy},
			{"Sent at", now.UTC().Format("2006-01-02 15:04 UTC")},
			{"SMTP host", smtpHost},
		},
	}
	if u := b.SettingsURL(); u != "" {
		e.Action = &Action{Label: "Open email settings", URL: u}
	}
	return e
}

// Once sends an alert unless one with the same key was sent within ttl.
func (n *Notifier) Once(ctx context.Context, key string, ttl time.Duration, e Email) {
	subject := e.Subject
	cfg, err := n.config(ctx)
	if err != nil {
		n.log.WarnContext(ctx, "load email settings failed", logging.Err(err))
		return
	}
	if !cfg.Enabled() {
		n.log.WarnContext(ctx, "alert (email not configured)", slog.String("subject", subject))
		return
	}
	ok, err := n.rdb.SetNX(ctx, dedupePrefix+key, time.Now().UTC().Format(time.RFC3339), ttl).Result()
	if err != nil {
		n.log.WarnContext(ctx, "alert dedupe failed; skipping", logging.Err(err))
		return
	}
	if !ok {
		return
	}
	if err := n.send(cfg, e); err != nil {
		// Release the key so the alert is retried on the next check.
		_ = n.rdb.Del(ctx, dedupePrefix+key).Err()
		n.log.ErrorContext(ctx, "send alert email failed", slog.String("subject", subject), logging.Err(err))
		return
	}
	n.log.InfoContext(ctx, "alert email sent", slog.String("subject", subject), slog.Int("recipients", len(cfg.To)))
}

func (n *Notifier) send(cfg SMTPConfig, e Email) error {
	msg, err := Message(cfg.From, cfg.To, e, n.Brand(), time.Now())
	if err != nil {
		return err
	}
	return deliver(cfg, msg)
}

func deliver(cfg SMTPConfig, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
	}
	if cfg.Port != 465 {
		// net/smtp upgrades with STARTTLS when the server advertises it.
		return smtp.SendMail(addr, auth, cfg.From, cfg.To, msg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 15 * time.Second},
		Config:    &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg.From); err != nil {
		return err
	}
	for _, to := range cfg.To {
		if err := c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ParseRecipients splits a comma-separated address list.
func ParseRecipients(s string) ([]string, error) {
	var out []string
	for _, a := range strings.Split(s, ",") {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !strings.Contains(a, "@") {
			return nil, errors.New("invalid email address: " + a)
		}
		out = append(out, a)
	}
	return out, nil
}
