package analytics

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	uaChrome   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	uaSafari   = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	uaEdge     = uaChrome + " Edg/141.0.0.0"
	uaFirefox  = "Mozilla/5.0 (X11; Linux x86_64; rv:145.0) Gecko/20100101 Firefox/145.0"
	uaBot      = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	uaHeadless = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/141.0.0.0 Safari/537.36"
)

func TestBrowserAndDevice(t *testing.T) {
	for ua, want := range map[string]string{uaChrome: "Chrome", uaSafari: "Safari", uaEdge: "Edge",
		uaFirefox: "Firefox", "SomethingElse/1.0": "Other"} {
		if got := browserOf(ua); got != want {
			t.Errorf("browserOf(%q) = %q, want %q", ua, got, want)
		}
	}
	cases := []struct {
		width int
		ua    string
		want  string
	}{{390, uaChrome, "mobile"}, {800, uaChrome, "tablet"}, {1440, uaChrome, "desktop"},
		{0, uaSafari, "mobile"}, {0, uaChrome, "desktop"}}
	for _, c := range cases {
		if got := deviceOf(c.width, c.ua); got != c.want {
			t.Errorf("deviceOf(%d) = %q, want %q", c.width, got, c.want)
		}
	}
}

func TestReferrerHost(t *testing.T) {
	for ref, want := range map[string]string{
		"":                                    "",
		"https://www.google.com/search?q=x":   "google.com",
		"https://t.co/abc":                    "t.co",
		"https://listening.example.com/":      "", // own site
		"https://WWW.Listening.Example.com/x": "",
		"android-app://com.slack":             "",
		"not a url":                           "",
		"https://" + strings.Repeat("a", 120) + ".com/": "",
	} {
		if got := referrerHost(ref, "listening.example.com:443"); got != want {
			t.Errorf("referrerHost(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestCleanPathAndCountry(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/": "/", "/?utm=x#y": "/", "/Stats": "/stats",
		"javascript:alert(1)": "/", "/" + strings.Repeat("x", 200): "/" + strings.Repeat("x", 99)} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"ca": "CA", "VN": "VN", "XX": "", "T1": "", "CAN": "", "": ""} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("CF-IPCountry", in)
		if got := countryOf(req); got != want {
			t.Errorf("countryOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func newPageTestRecorder() *Recorder {
	return NewRecorder(nil, nil, "salt", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func post(r *Recorder, body, ua string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	req.Header.Set("User-Agent", ua)
	req.Header.Set("CF-IPCountry", "CA")
	req.Host = "listening.example.com"
	rec := httptest.NewRecorder()
	r.EventsHandler(rec, req)
	return rec.Code
}

func TestEventsHandler(t *testing.T) {
	r := newPageTestRecorder()
	if c := post(r, `{"type":"pageview","path":"/","referrer":"https://www.google.com/","width":390}`, uaSafari); c != http.StatusAccepted {
		t.Fatalf("pageview status = %d", c)
	}
	if c := post(r, `{"type":"engagement","path":"/","width":390,"visible_ms":90000000}`, uaSafari); c != http.StatusAccepted {
		t.Fatalf("engagement status = %d", c)
	}
	// Bots and headless browsers are accepted but not counted.
	for _, ua := range []string{uaBot, uaHeadless, ""} {
		if c := post(r, `{"type":"pageview","path":"/"}`, ua); c != http.StatusAccepted {
			t.Fatalf("bot status = %d", c)
		}
	}
	for _, bad := range []string{`{`, `{"type":"click"}`, `{"type":"pageview","path":` + strings.Repeat(`"x`, 2000) + `}`} {
		if c := post(r, bad, uaChrome); c != http.StatusBadRequest && c != http.StatusRequestEntityTooLarge {
			t.Errorf("bad event %q... status = %d", bad[:min(len(bad), 20)], c)
		}
	}

	var views, visible int64
	for k, v := range r.pages {
		views += v.views
		visible += v.visibleMS
		if v.views > 0 && (k.referrer != "google.com" || k.device != "mobile" || k.browser != "Safari" || k.country != "CA") {
			t.Errorf("unexpected view dimensions %+v", k)
		}
	}
	if views != 1 {
		t.Errorf("views = %d, want 1 (bots must not count)", views)
	}
	if visible != maxVisibleMS {
		t.Errorf("visible = %d, want capped %d", visible, maxVisibleMS)
	}
	if len(r.visitors) != 1 {
		t.Errorf("visitor events = %d, want 1", len(r.visitors))
	}
}

func TestPageAggregator(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 30, 0, 0, time.UTC)
	spec, _ := parseRange("24h", now, time.UTC)
	a := newPageAggregator(spec)
	h := now.Truncate(time.Hour)
	a.add(pageRow{bucket: h, path: "/", device: "mobile", browser: "Safari", country: "CA", referrer: "google.com", views: 3})
	a.add(pageRow{bucket: h.Add(-2 * time.Hour), path: "/", device: "desktop", browser: "Chrome", country: "", views: 1})
	a.add(pageRow{bucket: h, path: "/", device: "mobile", browser: "Safari", country: "CA", visibleMS: 120_000})
	rep := a.report(now)
	if rep.Totals.Views != 4 || rep.Totals.AvgVisibleMS != 30_000 {
		t.Errorf("totals = %+v", rep.Totals)
	}
	if len(rep.Timeseries) != 24 || rep.Timeseries[23].Views != 3 || rep.Timeseries[21].Views != 1 {
		t.Errorf("timeseries = %+v", rep.Timeseries)
	}
	if rep.Devices[0] != (DimCount{Name: "mobile", Views: 3}) || len(rep.Referrers) != 2 {
		t.Errorf("devices = %+v referrers = %+v", rep.Devices, rep.Referrers)
	}
}
