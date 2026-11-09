package backupstatus

import (
	"regexp"
	"strconv"
)

// Built-in defaults of the backup container (BACKUP_TIME, BACKUP_KEEP_DAYS,
// BACKUP_KEEP_MIN).
const (
	DefaultTime     = "03:00"
	DefaultKeepDays = 30
	DefaultKeepMin  = 7
)

// Limits the backup container accepts for the dashboard settings.
const (
	MaxKeepDays = 3650
	MinKeepMin  = 1
	MaxKeepMin  = 1000
)

var hhmm = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// ValidTime reports whether s is a 24-hour HH:MM time.
func ValidTime(s string) bool { return hhmm.MatchString(s) }

// Validate checks dashboard backup settings and returns field → message
// for every invalid value (empty when all are valid).
func Validate(t string, keepDays, keepMin int) map[string]string {
	fields := map[string]string{}
	if !ValidTime(t) {
		fields["time"] = "must be a 24-hour time HH:MM (UTC)"
	}
	if keepDays < 0 || keepDays > MaxKeepDays {
		fields["keep_days"] = "must be between 0 (keep all) and " + strconv.Itoa(MaxKeepDays)
	}
	if keepMin < MinKeepMin || keepMin > MaxKeepMin {
		fields["keep_min"] = "must be between " + strconv.Itoa(MinKeepMin) + " and " + strconv.Itoa(MaxKeepMin)
	}
	return fields
}
