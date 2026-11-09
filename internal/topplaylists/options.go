package topplaylists

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultKeep is the default number of playlists kept per kind
	// (TOP_PLAYLISTS_KEEP).
	DefaultKeep = 3
	// DefaultVerify is the default time between two checks of the published
	// playlists against Spotify (TOP_PLAYLISTS_VERIFY).
	DefaultVerify = time.Hour
	// MinVerify is the shortest accepted check interval.
	MinVerify = 10 * time.Minute
	// MaxOwnerLen is the longest owner name used in playlist names, in runes.
	MaxOwnerLen = 40
)

// ParseKeep parses TOP_PLAYLISTS_KEEP: an integer >= 1, or 0 to never prune
// ("" = DefaultKeep). An invalid or negative value returns DefaultKeep
// together with the error.
func ParseKeep(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DefaultKeep, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return DefaultKeep, fmt.Errorf("TOP_PLAYLISTS_KEEP: %q is not a whole number >= 0", s)
	}
	return n, nil
}

// ParseVerify parses TOP_PLAYLISTS_VERIFY: a Go duration of at least
// MinVerify, or "0" / "off" to disable the checks ("" = DefaultVerify). An
// invalid or too short value returns DefaultVerify together with the error.
func ParseVerify(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "":
		return DefaultVerify, nil
	case "0", "off", "none":
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return DefaultVerify, fmt.Errorf("TOP_PLAYLISTS_VERIFY: invalid duration %q: %w", s, err)
	}
	if d == 0 {
		return 0, nil
	}
	if d < MinVerify {
		return DefaultVerify, fmt.Errorf("TOP_PLAYLISTS_VERIFY: %s is shorter than the minimum %s", d, MinVerify)
	}
	return d, nil
}

// ParseOwner trims an owner name and cuts it to MaxOwnerLen runes. A name
// that was cut is returned together with an error.
func ParseOwner(s string) (string, error) {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > MaxOwnerLen {
		cut := strings.TrimSpace(string(r[:MaxOwnerLen]))
		return cut, fmt.Errorf("TOP_PLAYLISTS_OWNER: longer than %d characters; using %q", MaxOwnerLen, cut)
	}
	return s, nil
}
