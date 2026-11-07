package admin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

func TestTopPlaylistsLive(t *testing.T) {
	h := newTopHarness(t, true)
	h.api.TopPlaylistsConfig.Live = []topplaylists.Period{topplaylists.Week, topplaylists.Year} // year is not enabled
	h.api.TopPlaylistsConfig.LiveRefresh = 2 * time.Hour
	row := createdRow()
	refreshed := time.Date(2026, 11, 4, 15, 0, 0, 0, ny)
	row.Live, row.RefreshedAt = true, &refreshed
	h.tops.rows[0] = row

	code, body := h.do(t, http.MethodGet, "/admin/api/top-playlists", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var data []map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &data)
	if string(data[0]["live"]) != "true" || string(data[0]["refreshed_at"]) != `"2026-11-04T20:00:00Z"` ||
		string(data[1]["live"]) != "false" || string(data[1]["refreshed_at"]) != "null" {
		t.Errorf("live=%s refreshed_at=%s", data[0]["live"], data[0]["refreshed_at"])
	}
	var st map[string]json.RawMessage
	_ = json.Unmarshal(body["status"], &st)
	if string(st["live_periods"]) != `["week"]` || string(st["live_refresh_seconds"]) != "7200" {
		t.Errorf("status = %s", body["status"])
	}

	// Regenerating a live row goes through Regenerate (a forced refresh).
	h.runner.genRec = row
	code, body = h.do(t, http.MethodPost, encodeComponentPath(startPath(row)+"/regenerate"), "")
	if code != http.StatusOK || len(h.runner.regCall) != 1 || len(h.audit.calls) != 1 {
		t.Fatalf("regenerate: %d %s calls=%v audit=%+v", code, errCode(t, body), h.runner.regCall, h.audit.calls)
	}
	var d map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &d)
	if string(d["live"]) != "true" {
		t.Errorf("regenerate data = %s", body["data"])
	}
}
