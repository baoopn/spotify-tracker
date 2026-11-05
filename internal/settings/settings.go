// Package settings persists dashboard-editable configuration in Postgres,
// encrypting secret fields. Values from the environment act as seeds: they are
// written once and whenever they change, while edits made in the dashboard
// win otherwise.
package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/spotify-tracker/internal/secrets"
)

const (
	keySpotify = "spotify"
	keyEmail   = "email"
)

// ErrNotFound means the setting has never been stored.
var ErrNotFound = errors.New("setting not found")

// Spotify holds the Spotify authorization state.
type Spotify struct {
	RefreshToken  string     `json:"-"`
	AuthorizedAt  *time.Time `json:"authorized_at"`
	Scopes        []string   `json:"scopes"`
	Source        string     `json:"source"` // env | dashboard
	AccountID     string     `json:"account_id,omitempty"`
	AccountName   string     `json:"account_name,omitempty"`
	LastRefreshAt *time.Time `json:"last_refresh_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	UpdatedAt     time.Time  `json:"-"`
}

// Email holds SMTP / alert settings.
type Email struct {
	Host            string    `json:"host"`
	Port            int       `json:"port"`
	Username        string    `json:"username"`
	Password        string    `json:"-"`
	From            string    `json:"from"`
	AlertTo         []string  `json:"alert_to"`
	ExpiryAlertDays []int     `json:"expiry_alert_days"`
	UpdatedAt       time.Time `json:"-"`
}

// Enabled reports whether alerts can be sent.
func (e Email) Enabled() bool { return e.Host != "" && e.From != "" && len(e.AlertTo) > 0 }

// DefaultExpiryAlertDays are the days-before-expiry at which alerts are sent.
var DefaultExpiryAlertDays = []int{14, 7, 3, 1}

// stored wraps a setting with encrypted secrets and the hash of the env seed
// it was last seeded from.
type stored[T any] struct {
	Data     T      `json:"data"`
	Secret   string `json:"secret,omitempty"`
	SeedHash string `json:"seed_hash,omitempty"`
}

// Store reads and writes settings.
type Store struct {
	pool *pgxpool.Pool
	box  *secrets.Box
}

// New creates a Store.
func New(pool *pgxpool.Pool, box *secrets.Box) *Store { return &Store{pool: pool, box: box} }

func load[T any](ctx context.Context, s *Store, key string) (stored[T], time.Time, error) {
	var raw []byte
	var updated time.Time
	var out stored[T]
	err := s.pool.QueryRow(ctx, `SELECT value, updated_at FROM settings WHERE key = $1`, key).Scan(&raw, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, updated, ErrNotFound
	}
	if err != nil {
		return out, updated, fmt.Errorf("load setting %s: %w", key, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, updated, fmt.Errorf("decode setting %s: %w", key, err)
	}
	return out, updated, nil
}

func save[T any](ctx context.Context, s *Store, key, actor string, v stored[T]) error {
	raw, err := json.Marshal(v) //nolint:gosec // Secret holds AES-GCM ciphertext, never plaintext
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by`,
		key, raw, actor)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}
	return nil
}

func seedHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ─── Spotify ──────────────────────────────────────────────────────────────────

// Spotify returns the Spotify settings (ErrNotFound if never set).
func (s *Store) Spotify(ctx context.Context) (Spotify, error) {
	st, updated, err := load[Spotify](ctx, s, keySpotify)
	if err != nil {
		return Spotify{}, err
	}
	tok, err := s.box.Open(st.Secret)
	if err != nil {
		return Spotify{}, err
	}
	st.Data.RefreshToken = tok
	st.Data.UpdatedAt = updated
	return st.Data, nil
}

// SaveSpotify stores Spotify settings.
func (s *Store) SaveSpotify(ctx context.Context, actor string, v Spotify) error {
	cur, _, err := load[Spotify](ctx, s, keySpotify)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	sealed, err := s.box.Seal(v.RefreshToken)
	if err != nil {
		return err
	}
	return save(ctx, s, keySpotify, actor, stored[Spotify]{Data: v, Secret: sealed, SeedHash: cur.SeedHash})
}

// SeedSpotify writes the env-provided token when nothing is stored yet or the
// env value changed since the last seed. It returns true when it wrote.
func (s *Store) SeedSpotify(ctx context.Context, refreshToken string, authorizedAt *time.Time) (bool, error) {
	if refreshToken == "" {
		return false, nil
	}
	hash := seedHash(refreshToken)
	cur, _, err := load[Spotify](ctx, s, keySpotify)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if err == nil && cur.SeedHash == hash {
		return false, nil
	}
	sealed, err := s.box.Seal(refreshToken)
	if err != nil {
		return false, err
	}
	return true, save(ctx, s, keySpotify, "env", stored[Spotify]{
		Data:     Spotify{AuthorizedAt: authorizedAt, Source: "env"},
		Secret:   sealed,
		SeedHash: hash,
	})
}

// ─── Email ────────────────────────────────────────────────────────────────────

// Email returns the email settings (ErrNotFound if never set).
func (s *Store) Email(ctx context.Context) (Email, error) {
	st, updated, err := load[Email](ctx, s, keyEmail)
	if err != nil {
		return Email{}, err
	}
	pw, err := s.box.Open(st.Secret)
	if err != nil {
		return Email{}, err
	}
	st.Data.Password = pw
	st.Data.UpdatedAt = updated
	if st.Data.ExpiryAlertDays == nil {
		st.Data.ExpiryAlertDays = DefaultExpiryAlertDays
	}
	if st.Data.AlertTo == nil {
		st.Data.AlertTo = []string{}
	}
	return st.Data, nil
}

// SaveEmail stores email settings.
func (s *Store) SaveEmail(ctx context.Context, actor string, v Email) error {
	cur, _, err := load[Email](ctx, s, keyEmail)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	sealed, err := s.box.Seal(v.Password)
	if err != nil {
		return err
	}
	return save(ctx, s, keyEmail, actor, stored[Email]{Data: v, Secret: sealed, SeedHash: cur.SeedHash})
}

// SeedEmail writes env-provided SMTP settings when nothing is stored yet or
// the env values changed since the last seed.
func (s *Store) SeedEmail(ctx context.Context, v Email) (bool, error) {
	if v.Host == "" {
		return false, nil
	}
	if v.ExpiryAlertDays == nil {
		v.ExpiryAlertDays = DefaultExpiryAlertDays
	}
	hash := seedHash(v.Host, fmt.Sprint(v.Port), v.Username, v.Password, v.From, fmt.Sprint(v.AlertTo))
	cur, _, err := load[Email](ctx, s, keyEmail)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if err == nil && cur.SeedHash == hash {
		return false, nil
	}
	sealed, err := s.box.Seal(v.Password)
	if err != nil {
		return false, err
	}
	return true, save(ctx, s, keyEmail, "env", stored[Email]{Data: v, Secret: sealed, SeedHash: hash})
}
