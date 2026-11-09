package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/domain"
	"github.com/baoopn/spotify-tracker/internal/topplaylists"
)

func prunedRow() domain.TopPlaylistRecord {
	r := createdRow()
	start := nov2.AddDate(0, 0, -21)
	pruned := published.Add(2 * time.Hour)
	r.PeriodStart, r.PeriodEnd = start.UTC(), start.AddDate(0, 0, 7).UTC()
	r.Name, r.Status, r.PrunedAt, r.SpotifyRemoved = "Top 50 · Week of Oct 12, 2026", domain.TopPlaylistPruned, &pruned, true
	r.LastError, r.NextAttemptAt, r.Attempts = "", nil, 0
	return r
}

func TestTopPlaylistsPruneFields(t *testing.T) {
	h := newTopHarness(t, true)
	h.api.TopPlaylistsConfig.Keep, h.api.TopPlaylistsConfig.Verify = 3, 90*time.Minute
	h.runner.owner = [2]string{"alex", topplaylists.OwnerSourceSpotify}
	restored, verified := published.Add(3*time.Hour), published.Add(4*time.Hour)
	h.tops.rows[0].RestoredAt, h.tops.rows[0].VerifiedAt = &restored, &verified
	h.tops.rows = append(h.tops.rows, prunedRow())

	code, body := h.do(t, http.MethodGet, "/admin/api/top-playlists", "")
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	var data []map[string]json.RawMessage
	_ = json.Unmarshal(body["data"], &data)
	if string(data[0]["restored_at"]) != `"2026-11-09T08:01:12Z"` || string(data[0]["verified_at"]) != `"2026-11-09T09:01:12Z"` ||
		string(data[0]["pruned_at"]) != "null" {
		t.Errorf("created row: %s %s %s", data[0]["restored_at"], data[0]["verified_at"], data[0]["pruned_at"])
	}
	if string(data[1]["restored_at"]) != "null" || string(data[1]["verified_at"]) != "null" {
		t.Errorf("pending row: %s %s", data[1]["restored_at"], data[1]["verified_at"])
	}
	if string(data[2]["status"]) != `"pruned"` || string(data[2]["pruned_at"]) != `"2026-11-09T07:01:12Z"` {
		t.Errorf("pruned row: %s %s", data[2]["status"], data[2]["pruned_at"])
	}
	var st map[string]json.RawMessage
	_ = json.Unmarshal(body["status"], &st)
	if string(st["keep"]) != "3" || string(st["verify_seconds"]) != "5400" ||
		string(st["owner"]) != `"alex"` || string(st["owner_source"]) != `"spotify"` {
		t.Errorf("status = %v", st)
	}

	off := newTopHarness(t, false)
	_, body = off.do(t, http.MethodGet, "/admin/api/top-playlists", "")
	_ = json.Unmarshal(body["status"], &st)
	if string(st["keep"]) != "0" || string(st["verify_seconds"]) != "0" ||
		string(st["owner"]) != `""` || string(st["owner_source"]) != `"none"` {
		t.Errorf("disabled status = %v", st)
	}
}

func TestTopPlaylistsVerifyEndpoint(t *testing.T) {
	h := newTopHarness(t, true)
	code, body := h.do(t, http.MethodPost, "/admin/api/top-playlists/verify", "")
	if code != http.StatusOK || h.runner.verifs != 1 || string(body["total"]) != "2" || body["status"] == nil {
		t.Fatalf("status %d verifs=%d body=%v", code, h.runner.verifs, body)
	}
	if len(h.audit.calls) != 1 || h.audit.calls[0].action != "top_playlists.verified" ||
		h.audit.calls[0].actor != "admin@example.com" {
		t.Errorf("audit = %+v", h.audit.calls)
	}

	h.runner.verErr = topplaylists.ErrBusy
	if code, body = h.do(t, http.MethodPost, "/admin/api/top-playlists/verify", ""); code != http.StatusConflict || errCode(t, body) != "busy" {
		t.Errorf("busy: %d %s", code, errCode(t, body))
	}
	h.runner.verErr = errors.New("boom")
	if code, _ = h.do(t, http.MethodPost, "/admin/api/top-playlists/verify", ""); code != http.StatusInternalServerError {
		t.Errorf("error: %d", code)
	}
	off := newTopHarness(t, false)
	if code, body = off.do(t, http.MethodPost, "/admin/api/top-playlists/verify", ""); code != http.StatusServiceUnavailable ||
		errCode(t, body) != "top_playlists_disabled" {
		t.Errorf("disabled: %d %s", code, errCode(t, body))
	}
}

func TestTopPlaylistsPruneErrors(t *testing.T) {
	h := newTopHarness(t, true)
	h.api.TopPlaylistsConfig.Keep = 3
	pruned := prunedRow()
	h.tops.rows = append(h.tops.rows, pruned)

	h.runner.genErr = topplaylists.ErrTooOld
	code, body := h.do(t, http.MethodPost, "/admin/api/top-playlists/generate", `{"period":"week","date":"2026-01-07"}`)
	var e apiError
	_ = json.Unmarshal(body["error"], &e)
	if code != http.StatusBadRequest || e.Code != "period_too_old" || !strings.Contains(e.Message, "only the newest 3 playlists per kind are kept") {
		t.Errorf("too old: %d %+v", code, e)
	}
	h.runner.genErr = topplaylists.ErrPruned
	if code, body = h.do(t, http.MethodPost, "/admin/api/top-playlists/generate", `{"period":"week","date":"2026-10-14"}`); code != http.StatusConflict || errCode(t, body) != "pruned" {
		t.Errorf("generate pruned: %d %s", code, errCode(t, body))
	}

	h.runner.genErr = nil
	regs := len(h.runner.regCall)
	if code, body = h.do(t, http.MethodPost, startPath(pruned)+"/regenerate", ""); code != http.StatusConflict || errCode(t, body) != "pruned" {
		t.Errorf("regenerate pruned: %d %s", code, errCode(t, body))
	}
	if len(h.runner.regCall) != regs {
		t.Error("pruned row regenerated")
	}
	if code, body = h.do(t, http.MethodPatch, startPath(pruned), `{"hidden":true}`); code != http.StatusConflict || errCode(t, body) != "pruned" {
		t.Errorf("hide pruned: %d %s", code, errCode(t, body))
	}
	if h.tops.hiddenArgs != "" || h.inval != 0 {
		t.Error("pruned row changed")
	}
	if code, _ = h.do(t, http.MethodPatch, startPath(createdRow()), `{"hidden":true}`); code != http.StatusOK {
		t.Errorf("hide created: %d", code)
	}
}
