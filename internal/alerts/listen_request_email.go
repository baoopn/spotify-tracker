package alerts

import (
	"time"

	"github.com/baoopn/spotify-tracker/internal/notify"
)

// ListenRequestEmail is the alert for a new "Request an invite" from
// /listen. Every value is the visitor's plain text: notify escapes it in
// the HTML part, and keeps the subject on one header line.
func ListenRequestEmail(name, email, message, country string, at time.Time, listenersURL string) notify.Email {
	facts := []notify.Fact{{Label: "Name", Value: name}, {Label: "Email", Value: email}}
	if country != "" {
		facts = append(facts, notify.Fact{Label: "Country", Value: country})
	}
	facts = append(facts, notify.Fact{Label: "Sent", Value: stampUTC(at)})
	e := notify.Email{
		Subject:   "Listen-along request from " + name,
		Preheader: name + " asked to listen along.",
		Tone:      notify.Info,
		Badge:     "Invite request",
		Title:     "Someone asked to listen along",
		Paragraphs: []string{
			"A visitor asked for an invite on the listen-along page. Nobody gets access until you invite them.",
		},
		Facts:  facts,
		Code:   message,
		Action: &notify.Action{Label: "Open Listeners", URL: listenersURL},
		Note:   "Invite or dismiss the request on the Listeners page.",
	}
	if message == "" {
		e.Paragraphs = append(e.Paragraphs, "They left no message.")
	}
	return e
}
