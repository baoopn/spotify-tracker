package httpapi

import (
	"net/http"
	"runtime"
	"time"
)

// zoneInfo is the server's view of a time zone right now. The server embeds
// the IANA database from its Go toolchain, which is often newer than the
// one bundled in browsers; clients use this to detect and correct stale
// browser tz data after a region changes its time-zone rules.
type zoneInfo struct {
	TZ            string    `json:"tz"`
	Abbreviation  string    `json:"abbreviation"`
	OffsetMinutes int       `json:"offset_minutes"`
	Offset        string    `json:"offset"` // "+05:30" / "-06:00", usable as an Intl timeZone
	Now           time.Time `json:"now"`
	Source        string    `json:"source"`
}

// GET /api/v1/timezone?tz=Europe/Berlin
func (h *Handler) timezone(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("tz")
	if name == "" || len(name) > 64 || name == "Local" {
		writeError(w, http.StatusBadRequest, "invalid_tz", "tz must be an IANA time zone name, e.g. Europe/Berlin")
		return
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_tz", "unknown time zone: "+name)
		return
	}
	now := time.Now().In(loc)
	abbr, off := now.Zone()
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, itemResponse[zoneInfo]{Data: zoneInfo{
		TZ: loc.String(), Abbreviation: abbr, OffsetMinutes: off / 60, Offset: formatOffset(off),
		Now: now, Source: "IANA tz database embedded in " + runtime.Version(),
	}})
}

// formatOffset renders seconds east of UTC as "+hh:mm".
func formatOffset(sec int) string {
	sign := byte('+')
	if sec < 0 {
		sign, sec = '-', -sec
	}
	h, m := sec/3600, (sec%3600)/60
	return string([]byte{sign, byte('0' + h/10), byte('0' + h%10), ':', byte('0' + m/10), byte('0' + m%10)})
}
