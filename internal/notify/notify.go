// Package notify sends operator alerts by email (SMTP), de-duplicated in
// Redis so each alert goes out once.
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
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

// Notifier sends alerts.
type Notifier struct {
	config ConfigFunc
	rdb    *redis.Client
	log    *slog.Logger
}

// New creates a Notifier. If the config is not Enabled, alerts are only logged.
func New(config ConfigFunc, rdb *redis.Client, log *slog.Logger) *Notifier {
	return &Notifier{config: config, rdb: rdb, log: log}
}

// Send delivers a message immediately to the given recipients (or the
// configured alert recipients when to is empty).
func (n *Notifier) Send(ctx context.Context, to []string, subject, body string) error {
	cfg, err := n.config(ctx)
	if err != nil {
		return err
	}
	if len(to) > 0 {
		cfg.To = to
	}
	if cfg.Host == "" || cfg.From == "" || len(cfg.To) == 0 {
		return errors.New("email is not configured: host, from and at least one recipient are required")
	}
	return send(cfg, subject, body)
}

// Once sends an alert unless one with the same key was sent within ttl.
func (n *Notifier) Once(ctx context.Context, key string, ttl time.Duration, subject, body string) {
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
	if err := send(cfg, subject, body); err != nil {
		// Release the key so the alert is retried on the next check.
		_ = n.rdb.Del(ctx, dedupePrefix+key).Err()
		n.log.ErrorContext(ctx, "send alert email failed", slog.String("subject", subject), logging.Err(err))
		return
	}
	n.log.InfoContext(ctx, "alert email sent", slog.String("subject", subject), slog.Int("recipients", len(cfg.To)))
}

func send(cfg SMTPConfig, subject, body string) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	msg := buildMessage(cfg.From, cfg.To, subject, body, time.Now())

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

func buildMessage(from string, to []string, subject, body string, now time.Time) []byte {
	clean := func(s string) string { return strings.NewReplacer("\r", "", "\n", " ").Replace(s) }
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", clean(from))
	fmt.Fprintf(&b, "To: %s\r\n", clean(strings.Join(to, ", ")))
	fmt.Fprintf(&b, "Subject: %s\r\n", clean(subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
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
