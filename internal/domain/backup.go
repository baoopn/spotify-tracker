package domain

import "time"

// Backup run statuses (backup_runs.status).
const (
	BackupRunning = "running"
	BackupSuccess = "success"
	BackupFailed  = "failed"
)

// Backup request kinds (backup_requests.kind).
const (
	BackupRequestBackup  = "backup"
	BackupRequestRefresh = "refresh"
)

// BackupRun is one backup attempt recorded by the backup container.
type BackupRun struct {
	ID          int64
	Trigger     string // schedule | manual | start | retry
	Status      string // running | success | failed
	StartedAt   time.Time
	FinishedAt  *time.Time
	Name        *string
	SizeBytes   *int64
	SHA256      *string
	Tables      *int
	Error       *string
	RequestedBy *string
}

// BackupObject is one backup in the remote listing snapshot.
type BackupObject struct {
	Name      string
	SizeBytes int64
	CreatedAt time.Time
}

// BackupAgent is the backup container's heartbeat (a single row). It holds
// no secrets: the destination is redacted and recipients are public keys.
type BackupAgent struct {
	LastSeenAt     time.Time
	Version        *string
	Configured     bool
	ConfigError    *string
	Destination    *string
	Recipients     []string
	BackupTime     *string
	KeepDays       *int
	KeepMin        *int
	Paused         bool
	NextRunAt      *time.Time
	SettingsSource string // env | admin
}

// BackupRequest is a request from the dashboard for the backup container.
type BackupRequest struct {
	ID          int64
	Kind        string // backup | refresh
	RequestedAt time.Time
	RequestedBy *string
	ClaimedAt   *time.Time
}
