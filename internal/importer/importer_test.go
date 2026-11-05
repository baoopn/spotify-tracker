package importer

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestRelationID(t *testing.T) {
	cases := map[string]string{
		`"abc"`:           "abc",
		`{"id":"abc"}`:    "abc",
		`null`:            "",
		``:                "",
		`{"name":"x"}`:    "",
		`{"id":{"id":1}}`: "",
	}
	for in, want := range cases {
		if got := relationID(json.RawMessage(in)); got != want {
			t.Errorf("relationID(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestJunctionIDs(t *testing.T) {
	rows := []json.RawMessage{
		json.RawMessage(`{"id":1,"tracks_id":"t","artists_id":"a1"}`),
		json.RawMessage(`{"id":2,"tracks_id":"t","artists_id":{"id":"a2","name":"x"}}`),
		json.RawMessage(`{"id":3,"tracks_id":"t","artists_id":"a1"}`),
	}
	if got, want := junctionIDs(rows), []string{"a1", "a2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("junctionIDs = %v, want %v", got, want)
	}
}

func TestParseGenres(t *testing.T) {
	direct := parseGenres(json.RawMessage(`[{"root":"pop","sub":["k-pop"]},{"root":"","sub":[]}]`))
	if len(direct) != 1 || direct[0].Root != "pop" {
		t.Errorf("direct = %+v", direct)
	}
	encoded := parseGenres(json.RawMessage(`"[{\"root\":\"rock\"}]"`))
	if len(encoded) != 1 || encoded[0].Root != "rock" || encoded[0].Sub == nil {
		t.Errorf("encoded = %+v", encoded)
	}
	if parseGenres(json.RawMessage(`null`)) != nil {
		t.Error("null should be nil")
	}
}

func TestParseTime(t *testing.T) {
	want := time.Date(2026, 3, 16, 12, 34, 56, 0, time.UTC)
	for _, s := range []string{"2026-03-16T12:34:56Z", "2026-03-16T12:34:56.000Z", "2026-03-16T12:34:56", "2026-03-16 12:34:56"} {
		got, err := parseTime(s)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, %v", s, got, err)
		}
	}
	if _, err := parseTime("yesterday"); err == nil {
		t.Error("expected error")
	}
}

func TestFlexNumbers(t *testing.T) {
	var v struct {
		F *flexFloat `json:"f"`
		I *flexInt   `json:"i"`
		N *flexFloat `json:"n"`
	}
	if err := json.Unmarshal([]byte(`{"f":"0.5","i":"7","n":null}`), &v); err != nil {
		t.Fatal(err)
	}
	if *v.F.ptr() != 0.5 || *v.I.ptr32() != 7 || v.N.ptr() != nil {
		t.Errorf("got %+v", v)
	}
}

func TestToHistoryRow(t *testing.T) {
	five, dur := flexInt(5), flexInt(215000)
	upd := "2026-03-16T12:40:00Z"
	row, ok := toHistoryRow(dHistory{TrackID: json.RawMessage(`"t1"`), PlayedAt: "2026-03-16T12:34:56Z",
		UpdatedAt: &upd, ReplayCount: &five, DurationMS: &dur})
	if !ok || row.trackID != "t1" || row.replays != 5 || row.durationMS != 215000 || row.updatedAt.Minute() != 40 {
		t.Errorf("got %+v, %v", row, ok)
	}
	row, ok = toHistoryRow(dHistory{TrackID: json.RawMessage(`"t1"`), PlayedAt: "2026-03-16 12:34:56"})
	if !ok || row.replays != 1 || !row.updatedAt.Equal(row.playedAt) {
		t.Errorf("defaults: got %+v, %v", row, ok)
	}
	if _, ok := toHistoryRow(dHistory{TrackID: json.RawMessage(`null`), PlayedAt: "2026-03-16T12:34:56Z"}); ok {
		t.Error("missing track should be skipped")
	}
	if _, ok := toHistoryRow(dHistory{TrackID: json.RawMessage(`"t1"`), PlayedAt: "soon"}); ok {
		t.Error("bad time should be skipped")
	}
}

func TestCompletenessPicksRicherDuplicate(t *testing.T) {
	half := flexFloat(0.5)
	sparse := dFeatures{Energy: &half}
	rich := dFeatures{Energy: &half, Tempo: &half, Valence: &half, Genres: json.RawMessage(`[{"root":"pop"}]`)}
	if completeness(rich) <= completeness(sparse) {
		t.Errorf("completeness rich=%d sparse=%d", completeness(rich), completeness(sparse))
	}
	if completeness(dFeatures{}) != 0 {
		t.Error("empty row should score 0")
	}
}
