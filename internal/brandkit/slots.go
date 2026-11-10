// Package brandkit serves the listening brand files (logos, icons,
// favicons, og:image, email logo) from the baoopn brand API, with the
// repository's static copies as the fallback.
//
// A Cache polls GET {BRAND_API_URL} and keeps the last good copy forever; a
// failed refresh never clears it. Mount registers /brand/{slot}, the root
// favicon files and the web manifests.
package brandkit

import (
	"mime"
	"strings"
)

// Kind is the file type a slot must have.
type Kind int

// Slot kinds.
const (
	SVG Kind = iota + 1
	PNG
	ICO
)

// contentType is the Content-Type used when serving a static fallback.
func (k Kind) contentType() string {
	switch k {
	case SVG:
		return "image/svg+xml"
	case PNG:
		return "image/png"
	case ICO:
		return "image/x-icon"
	}
	return "application/octet-stream"
}

// ext is the file extension of the kind's static fallbacks.
func (k Kind) ext() string {
	switch k {
	case SVG:
		return ".svg"
	case PNG:
		return ".png"
	case ICO:
		return ".ico"
	}
	return ""
}

// accepts reports whether the API's content type fits the kind. Parameters
// (e.g. charset) are ignored and the match is case-insensitive.
func (k Kind) accepts(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch strings.ToLower(mt) {
	case "image/svg+xml":
		return k == SVG
	case "image/png":
		return k == PNG
	case "image/x-icon", "image/vnd.microsoft.icon":
		return k == ICO
	}
	return false
}

// Slot is one brand file.
type Slot struct {
	// Name is the API's slot key, e.g. "mark_primary".
	Name string
	Kind Kind
	// Fallback is the path of the static copy on the public site.
	Fallback string
	// Root marks files served at their Fallback path (favicons, og:image).
	Root bool
}

// EmailLogoSlot is the slot of the alert email header wordmark.
const EmailLogoSlot = "email_logo"

var (
	slots      = buildSlots()
	slotByName = indexSlots(slots)
)

func buildSlots() []Slot {
	out := []Slot{
		{"favicon_ico", ICO, "/favicon.ico", true},
		{"favicon_svg", SVG, "/favicon.svg", true},
		{"apple_touch_icon", PNG, "/apple-touch-icon.png", true},
		{"favicon_16", PNG, "/favicon-16.png", true},
		{"favicon_32", PNG, "/favicon-32.png", true},
		{"favicon_48", PNG, "/favicon-48.png", true},
		{"icon_192", PNG, "/icon-192.png", true},
		{"icon_512", PNG, "/icon-512.png", true},
		{"icon_maskable_512", PNG, "/icon-maskable-512.png", true},
		{"og_image", PNG, "/og-image.png", true},
		{EmailLogoSlot, PNG, "/email/listening-wordmark-primary@2x.png", false},
	}
	for _, kind := range []string{"mark", "wordmark"} {
		for _, v := range []string{"primary", "inverse", "navy", "cream"} {
			out = append(out, Slot{kind + "_" + v, SVG, "/logo/listening-" + kind + "-" + v + ".svg", false})
		}
	}
	for _, name := range []string{"headphones", "now_playing", "now_playing_static", "paused", "playlist", "track"} {
		for _, tone := range []string{"navy", "cream"} {
			out = append(out, Slot{"icon_" + name + "_" + tone, SVG,
				"/icons/" + strings.ReplaceAll(name, "_", "-") + "-" + tone + ".svg", false})
		}
	}
	return out
}

func indexSlots(ss []Slot) map[string]Slot {
	m := make(map[string]Slot, len(ss))
	for _, s := range ss {
		m[s.Name] = s
	}
	return m
}

// Slots returns every slot in a stable order (a copy).
func Slots() []Slot { return append([]Slot(nil), slots...) }

// SlotByName returns the slot with the given API key.
func SlotByName(name string) (Slot, bool) {
	s, ok := slotByName[name]
	return s, ok
}
