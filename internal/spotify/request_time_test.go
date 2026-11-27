package spotify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// slowTokens takes refreshDelay to refresh a token.
type slowTokens struct{ refreshDelay time.Duration }

func (s *slowTokens) AccessToken(_ context.Context, force bool) (string, error) {
	if force {
		time.Sleep(s.refreshDelay)
		return "fresh", nil
	}
	return "at", nil
}

// FetchedAt is the midpoint of the HTTP request that answered: a 401, the
// token refresh after it and the retry's token lookup are not part of it.
func TestFetchedAtIsMidpointOfAnsweringRequest(t *testing.T) {
	const answerDelay = 200 * time.Millisecond
	var answerStart time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			time.Sleep(150 * time.Millisecond)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		answerStart = time.Now()
		time.Sleep(answerDelay)
		_, _ = w.Write([]byte(playingBody))
	}))
	t.Cleanup(srv.Close)
	p := NewPlayer(&slowTokens{refreshDelay: 300 * time.Millisecond}, srv.URL+"/v1", nil)

	pb, rt, err := p.CurrentlyPlayingTimed(context.Background())
	if err != nil || pb == nil {
		t.Fatalf("got %v, %v", pb, err)
	}
	if rt.Sent.After(answerStart) || rt.Received.Before(answerStart.Add(answerDelay)) {
		t.Errorf("request time %v..%v does not cover the answer at %v", rt.Sent, rt.Received, answerStart)
	}
	want := answerStart.Add(answerDelay / 2)
	if d := pb.FetchedAt.Sub(want); d < -40*time.Millisecond || d > 40*time.Millisecond {
		t.Errorf("fetched_at is %v from the answering request's midpoint", d)
	}
	if !pb.FetchedAt.Equal(rt.Mid()) || pb.FetchedAt.Location() != time.UTC {
		t.Errorf("fetched_at %v, mid %v", pb.FetchedAt, rt.Mid())
	}
	// Nothing playing still reports the timing.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(empty.Close)
	pb, rt, err = NewPlayer(&slowTokens{}, empty.URL+"/v1", nil).CurrentlyPlayingTimed(context.Background())
	if err != nil || pb != nil || rt.Mid().IsZero() {
		t.Errorf("204 = %v %+v %v", pb, rt, err)
	}
}

func TestRequestTimeMid(t *testing.T) {
	if !(RequestTime{}).Mid().IsZero() {
		t.Error("zero request time has a midpoint")
	}
	a := time.Date(2026, 11, 27, 10, 0, 0, 0, time.FixedZone("x", 3600))
	if got := (RequestTime{Sent: a, Received: a.Add(time.Second)}).Mid(); !got.Equal(a.Add(500*time.Millisecond)) || got.Location() != time.UTC {
		t.Errorf("mid = %v", got)
	}
}
