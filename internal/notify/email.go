package notify

import (
	"bytes"
	"embed"
	"errors"
	htmltemplate "html/template"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Tone sets the colour of an email's chip and top stripe.
type Tone string

// Tones: info (lilac), warning (butter), danger (rose), success (mint).
const (
	Info    Tone = "info"
	Warning Tone = "warning"
	Danger  Tone = "danger"
	Success Tone = "success"
)

func (t Tone) valid() bool {
	switch t {
	case Info, Warning, Danger, Success:
		return true
	}
	return false
}

// Fact is one labelled value in an email's facts table.
type Fact struct{ Label, Value string }

// Action is the email's call-to-action button.
type Action struct{ Label, URL string }

// Email is one alert. All fields are plain text; the HTML part escapes them.
type Email struct {
	Subject   string
	Preheader string
	Tone      Tone
	// Badge is the chip text, e.g. "Expires in 7 days".
	Badge      string
	Title      string
	Paragraphs []string
	Facts      []Fact
	// Code is an optional monospace block, e.g. an error message.
	Code   string
	Action *Action
	// Note is small text under the button.
	Note string
}

// LogoPath is where the header wordmark is served on the public site.
const LogoPath = "/email/listening-wordmark-primary@2x.png"

// The wordmark PNG is 640 × 264 (2x); it is shown at 160 × 66 CSS px (the
// brand guide asks for at least 120 px wide).
const (
	logoWidth  = 160
	logoHeight = 66
)

// Brand is the site the emails speak for, derived from PUBLIC_URL.
type Brand struct {
	// URL is the http(s) origin without a trailing slash ("" when PUBLIC_URL
	// is not an http(s) URL).
	URL string
	// Host is URL's host name, shown in the footer.
	Host string
	// LogoURL is the absolute URL of the header wordmark ("" for none).
	LogoURL string
	// LogoHeight is the wordmark's shown height in CSS px at 160 px wide
	// (0 = 66, the default wordmark's ratio).
	LogoHeight int
}

// LogoHeightFor is the shown height of a w × h logo at 160 px wide. It is
// 66 when the size is unknown or the result falls outside 16..320 px.
func LogoHeightFor(w, h int) int {
	if w <= 0 || h <= 0 || w > 1<<20 || h > 1<<20 {
		return logoHeight
	}
	got := (logoWidth*h + w/2) / w
	if got < 16 || got > 320 {
		return logoHeight
	}
	return got
}

// WithLogo returns b with the header wordmark at logoURL (w × h px, used for
// the aspect ratio). An unsafe logoURL leaves b unchanged.
func (b Brand) WithLogo(logoURL string, w, h int) Brand {
	if safeURL(logoURL) == "" {
		return b
	}
	b.LogoURL = logoURL
	b.LogoHeight = LogoHeightFor(w, h)
	return b
}

func (b Brand) logoH() int {
	if b.LogoHeight <= 0 {
		return logoHeight
	}
	return b.LogoHeight
}

// ErrLogoURL means EMAIL_LOGO_URL is not an absolute http(s) URL.
var ErrLogoURL = errors.New("EMAIL_LOGO_URL must be an absolute http(s) URL")

// NewBrand derives the brand from the public URL. logoURL overrides the
// default wordmark location ({publicURL}/email/…png); an invalid override
// returns the default brand together with ErrLogoURL.
func NewBrand(publicURL, logoURL string) (Brand, error) {
	b := Brand{}
	if u := strings.TrimRight(strings.TrimSpace(publicURL), "/"); safeURL(u) != "" {
		p, _ := url.Parse(u)
		b.URL = u
		b.Host = strings.TrimPrefix(p.Hostname(), "www.")
		b.LogoURL = u + LogoPath
	}
	if logoURL = strings.TrimSpace(logoURL); logoURL != "" {
		if safeURL(logoURL) == "" {
			return b, ErrLogoURL
		}
		b.LogoURL = logoURL
	}
	return b, nil
}

// SettingsURL is the admin page where alert recipients are changed.
func (b Brand) SettingsURL() string {
	if b.URL == "" {
		return ""
	}
	return b.URL + "/admin/settings/email"
}

// safeURL returns u when it is an absolute http(s) URL without credentials
// or characters that could break out of an attribute, else "".
func safeURL(u string) string {
	if u == "" || strings.ContainsAny(u, " \t\r\n\"'<>\\`") {
		return ""
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil {
		return ""
	}
	return u
}

// maxCode is how many characters of Code are shown.
const maxCode = 500

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

//go:embed templates/email.html.tmpl
var templateFS embed.FS

var htmlTmpl = htmltemplate.Must(htmltemplate.New("email.html.tmpl").Option("missingkey=error").Funcs(htmltemplate.FuncMap{
	"msoHead":     func() htmltemplate.HTML { return msoHead },
	"msoOpen":     func() htmltemplate.HTML { return msoOpen },
	"msoClose":    func() htmltemplate.HTML { return msoClose },
	"notMsoOpen":  func() htmltemplate.HTML { return notMsoOpen },
	"notMsoClose": func() htmltemplate.HTML { return notMsoClose },
	"vmlButton":   vmlButton,
	"preheaderPad": func() htmltemplate.HTML {
		return htmltemplate.HTML(strings.Repeat("&#847;&zwnj;&nbsp;", 30)) //nolint:gosec // constant markup
	},
}).ParseFS(templateFS, "templates/email.html.tmpl"))

// view is what the HTML template sees: validated, ready-to-show values.
type view struct {
	Email
	Brand       Brand
	Tone        tone
	ActionURL   string
	Code        string
	SettingsURL string
	LogoW       int
	LogoH       int
}

// tone is the chip/stripe colours of a Tone in light mode, plus the class
// that switches them in dark mode.
type tone struct{ Class, Fill string }

var tones = map[Tone]tone{
	Info:    {"t-info", "#e3d7fb"},
	Warning: {"t-warning", "#fbedb3"},
	Danger:  {"t-danger", "#f9cfdc"},
	Success: {"t-success", "#c8ecd9"},
}

func (e Email) view(b Brand) view {
	t := e.Tone
	if !t.valid() {
		t = Info
	}
	v := view{Email: e, Brand: b, Tone: tones[t], Code: truncate(e.Code, maxCode),
		SettingsURL: b.SettingsURL(), LogoW: logoWidth, LogoH: b.logoH()}
	if e.Action != nil {
		v.ActionURL = safeURL(e.Action.URL)
	}
	return v
}

// Render returns the plain-text and HTML parts of e.
func (e Email) Render(b Brand) (text, html string, err error) {
	var h bytes.Buffer
	if err := htmlTmpl.Execute(&h, e.view(b)); err != nil {
		return "", "", err
	}
	return e.text(b), h.String(), nil
}

// text is the plain-text part: the same content as the HTML.
func (e Email) text(b Brand) string {
	v := e.view(b)
	var s strings.Builder
	line := func(l string) { s.WriteString(l + "\n") }
	if e.Badge != "" {
		line("[" + e.Badge + "]")
	}
	if e.Title != "" {
		line(e.Title)
		line(strings.Repeat("=", min(utf8.RuneCountInString(e.Title), 60)))
	}
	for _, p := range e.Paragraphs {
		line("")
		line(p)
	}
	if len(e.Facts) > 0 {
		line("")
		for _, f := range e.Facts {
			line(f.Label + ": " + f.Value)
		}
	}
	if v.Code != "" {
		line("")
		for _, l := range strings.Split(v.Code, "\n") {
			line("    " + l)
		}
	}
	if e.Action != nil {
		line("")
		if v.ActionURL != "" {
			line(e.Action.Label + ":")
			line("  " + v.ActionURL)
		} else {
			line(e.Action.Label)
		}
	}
	if e.Note != "" {
		line("")
		line(e.Note)
	}
	line("")
	line("-- ")
	if b.URL != "" {
		line("Sent by spotify-tracker · " + b.Host + " (" + b.URL + ")")
	} else {
		line("Sent by spotify-tracker")
	}
	const why = "You get these because you're an alert recipient. Change it in Settings → Email alerts"
	if v.SettingsURL != "" {
		line(why + ":")
		line("  " + v.SettingsURL)
	} else {
		line(why + ".")
	}
	return s.String()
}

// Outlook-only markup. html/template drops comments, so conditional
// comments come from these constants (none carries data).
const (
	msoHead = `<!--[if mso]><noscript><xml><o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml></noscript>` +
		`<style>body,table,td,div,a,p{font-family:Arial,Helvetica,sans-serif !important;}</style><![endif]-->`
	msoOpen     = `<!--[if mso]><table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" align="center"><tr><td><![endif]-->`
	msoClose    = `<!--[if mso]></td></tr></table><![endif]-->`
	notMsoOpen  = `<!--[if !mso]><!-->`
	notMsoClose = `<!--<![endif]-->`
)

// vmlButton is the Outlook (Word engine) version of the CTA: a VML
// rounded rectangle. href and label are escaped here because the template
// does not look inside comments.
func vmlButton(href, label string) htmltemplate.HTML {
	if safeURL(href) == "" {
		return ""
	}
	w := max(utf8.RuneCountInString(label)*9+64, 200)
	return htmltemplate.HTML(`<!--[if mso]><v:roundrect xmlns:v="urn:schemas-microsoft-com:vml" xmlns:w="urn:schemas-microsoft-com:office:word" href="` + //nolint:gosec // values escaped
		htmltemplate.HTMLEscapeString(href) + `" style="height:48px;v-text-anchor:middle;width:` + strconv.Itoa(w) +
		`px;" arcsize="50%" stroke="f" fillcolor="#0b0537"><w:anchorlock/><center style="color:#fff5eb;font-family:Arial,Helvetica,sans-serif;font-size:16px;font-weight:bold;">` +
		htmltemplate.HTMLEscapeString(label) + `</center></v:roundrect><![endif]-->`)
}
