package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const keyBackup = "backup"

// Backup holds the dashboard-editable backup schedule. It is stored as plain
// JSON (no secrets, no wrapper) because the backup container reads it
// directly: {"time":"03:00","keep_days":30,"keep_min":7,"paused":false}.
// Absent fields fall back to the container's environment, then to its
// built-in defaults.
type Backup struct {
	Time      string    `json:"time,omitempty"` // HH:MM UTC
	KeepDays  *int      `json:"keep_days,omitempty"`
	KeepMin   *int      `json:"keep_min,omitempty"`
	Paused    bool      `json:"paused"`
	UpdatedAt time.Time `json:"-"`
}

// Backup returns the backup settings (ErrNotFound if never set).
func (s *Store) Backup(ctx context.Context) (Backup, error) {
	var raw []byte
	var out Backup
	err := s.pool.QueryRow(ctx, `SELECT value, updated_at FROM settings WHERE key = $1`, keyBackup).
		Scan(&raw, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Backup{}, ErrNotFound
	}
	if err != nil {
		return Backup{}, fmt.Errorf("load setting %s: %w", keyBackup, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Backup{}, fmt.Errorf("decode setting %s: %w", keyBackup, err)
	}
	return out, nil
}

// SaveBackup stores the backup settings.
func (s *Store) SaveBackup(ctx context.Context, actor string, v Backup) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by`,
		keyBackup, raw, actor)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", keyBackup, err)
	}
	return nil
}

// DeleteBackup removes the backup settings, so the backup container falls
// back to its environment and built-in defaults. Deleting settings that
// were never saved is not an error.
func (s *Store) DeleteBackup(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM settings WHERE key = $1`, keyBackup); err != nil {
		return fmt.Errorf("delete setting %s: %w", keyBackup, err)
	}
	return nil
}
