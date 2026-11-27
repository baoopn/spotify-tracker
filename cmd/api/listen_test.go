package main

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/spotify-tracker/internal/config"
)

func listenCfg() config.API {
	return config.API{
		PublicURL: "https://listening.example.com/",
		Admin: config.Admin{ClerkSecretKey: "sk_test_x", ClerkPublishableKey: "pk_test_Y2xlcmsuZXhhbXBsZS5jb20k",
			AdminRole: "admin"},
		Listen: config.Listen{Enabled: true, AudioURLTTL: 10 * time.Minute, AudioExtensions: []string{"mp3"},
			S3: config.S3{Endpoint: "https://s3.example.com", Region: "us-east-1", Bucket: "music",
				AccessKeyID: "id", SecretAccessKey: "secret", ForcePathStyle: true}},
	}
}

func TestListenPageCSP(t *testing.T) {
	page := publicSiteOptions(listenCfg()).PageCSP["listen.html"]
	if page == nil {
		t.Fatal("no listen.html CSP")
	}
	if !strings.Contains(page["script-src"], "https://clerk.example.com") || !strings.Contains(page["connect-src"], "https://clerk.example.com") ||
		!strings.Contains(page["frame-src"], "https://challenges.cloudflare.com") || page["img-src"] != "https://img.clerk.com" ||
		page["media-src"] != "https://s3.example.com" {
		t.Errorf("listen CSP = %v", page)
	}
	off := listenCfg()
	off.Listen.Enabled = false
	if publicSiteOptions(off).PageCSP != nil {
		t.Error("listen CSP while disabled")
	}
	noS3 := listenCfg()
	noS3.Listen.S3 = config.S3{}
	if _, ok := publicSiteOptions(noS3).PageCSP["listen.html"]["media-src"]; ok {
		t.Error("media-src without storage")
	}
}

func TestListenOptions(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	cfg := listenCfg()
	o := listenOptions(cfg, nil, nil, nil, nil, nil, log)
	if !o.Enabled || o.Auth != nil || o.Audio == nil {
		t.Errorf("options = %+v", o)
	}
	cfg.Listen.S3 = config.S3{}
	if o := listenOptions(cfg, nil, nil, nil, nil, nil, log); o.Audio != nil {
		t.Error("typed nil locator")
	}
	if info := listenInfo(cfg); info.URL != "https://listening.example.com/listen" || !info.Enabled || info.AudioConfigured {
		t.Errorf("info = %+v", info)
	}
}
