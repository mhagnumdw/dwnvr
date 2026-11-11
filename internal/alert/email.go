package alert

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"image/color"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/email"
)

// Email is the notifier that sends the alert by e-mail: through the smtp
// section of dwnvr.yaml, the same as the set-password link, to the
// alerts.email.to list, in a single message to everyone.
type Email struct {
	sender email.Sender
	to     []string
	// publicURL is server.publicUrl, the base of the link to the recording;
	// empty, the e-mail goes out without the button.
	publicURL string
}

// NewEmail builds the notifier with the smtp section on and at least one
// address in the list; without either, it returns nil, and the alert stays
// only in the record. The list comes already checked by config.Load.
func NewEmail(cfg *config.Config) (Notifier, error) {
	if !cfg.SMTP.Enabled() || len(cfg.Alerts.Email.To) == 0 {
		return nil, nil
	}
	smtp, err := email.NewSMTP(cfg.SMTP)
	if err != nil {
		return nil, err
	}
	return &Email{sender: smtp, to: cfg.Alerts.Email.To, publicURL: cfg.Server.PublicURL}, nil
}

// NotifierEmail is the e-mail notifier's name, in Notifiers and in its state.
const NotifierEmail = "email"

func (e *Email) Name() string { return NotifierEmail }

// MissingForEmail says the dwnvr.yaml sections missing for the alert to go
// out by e-mail, in the order "alerts.email.to", "smtp", and nothing when the
// notifier gets built. A rejected smtp section counts as missing. The rules
// are NewEmail's.
func MissingForEmail(cfg *config.Config) []string {
	var missing []string
	if len(cfg.Alerts.Email.To) == 0 {
		missing = append(missing, "alerts.email.to")
	}
	if !cfg.SMTP.Enabled() {
		missing = append(missing, "smtp")
	}
	return missing
}

// Send builds the alert message and hands it to the server, within the ctx
// deadline and email.SendTimeout, whichever comes first.
func (e *Email) Send(ctx context.Context, a Alert) error {
	m, err := alertMessage(a, e.to, e.publicURL)
	if err != nil {
		return err
	}
	return e.sender.Send(ctx, m)
}

// familyDisplayNames are the names the message shows, the screen's.
var familyDisplayNames = map[detect.Family]string{
	detect.Person:  "Pessoa",
	detect.Vehicle: "Veículo",
	detect.Animal:  "Animal",
}

var weekdays = [...]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira",
	"quinta-feira", "sexta-feira", "sábado"}

//go:embed email.html
var alertHTML string

var alertTemplate = template.Must(template.New("email.html").Parse(alertHTML))

// logoPNG is the dwnvr icon in the e-mail header, a 48 px PNG for density-2
// screens: Gmail and Outlook don't show SVG. It comes from
// web/public/icon-192.png, which is favicon.svg as PNG:
//
//	magick web/public/icon-192.png -resize 48x48 -strip internal/alert/logo.png
//
//go:embed logo.png
var logoPNG []byte

// frameID and logoID are the cid: by which the HTML shows the frame and the
// logo.
const (
	frameID = "quadro@dwnvr"
	logoID  = "marca@dwnvr"
)

// alertBody is what email.html gets. Every text arrives ready, and
// html/template escapes it: the camera name and the area names come from the
// user.
type alertBody struct {
	Subject, Preview, Title, Subtitle, Footer, Camera string
	// Padding only repeats an invisible character after the preview, so the
	// mail reader doesn't fill in the preview with the start of the body.
	Padding []struct{}
	Objects []emailObject
	// Logo is the cid of the header icon; Image, the frame's, empty without a frame.
	Logo, Image, ImageAlt string
	// Link opens the recording at the instant; empty without server.publicUrl.
	Link template.URL
}

type emailObject struct {
	Name, Score, Where string
	Color              template.CSS
}

// alertMessage writes an alert's e-mail, in HTML and in text. The subject
// says what matters at a glance, like "[Alerta] Garagem(Portão): pessoa e
// veículo às 14:32", with the areas some object fell in between parentheses,
// without them when the alert applies to the whole image, and the families in
// priority order, which is the objects' (the Recorder sorts them). The body
// carries the dwnvr logo at the top, the frame, the day, each object and the
// areas it fell in, and the button that opens the recording at the instant,
// when server.publicUrl says dwnvr's address.
func alertMessage(a Alert, to []string, publicURL string) (email.Message, error) {
	// The time is the server's, the same as the recordings' day and the alert
	// schedules.
	onset := time.UnixMilli(a.AtMs).In(time.Local)
	when := fmt.Sprintf("%s, %s às %s", weekdays[onset.Weekday()],
		onset.Format("02/01/2006"), onset.Format("15:04:05"))
	families := familyNames(a.Objects)
	title := capitalize(joinNames(families))
	areas := areasOf(a)
	whereInSubject := a.CameraName
	if len(areas) > 0 {
		whereInSubject += "(" + joinNames(areas) + ")"
	}

	c := alertBody{
		// The subject is a single line, since sending rejects a subject with a
		// line break: one that comes in a name written by hand in cameras.json
		// becomes a space.
		Subject: oneLine(fmt.Sprintf("[Alerta] %s: %s às %s", whereInSubject, joinNames(families),
			onset.Format("15:04"))),
		Title: title,
		// In the HTML the date and the time don't split across a phone's line break.
		Subtitle: fmt.Sprintf("Câmera %s · %s", a.CameraName,
			strings.Replace(when, " às ", "\u00a0às\u00a0", 1)),
		Footer:  "Alerta da câmera " + a.CameraName,
		Preview: when,
		Camera:  a.CameraName,
		Logo:    logoID,
	}
	if len(areas) > 0 {
		c.Preview = fmt.Sprintf("%s %s · %s", plural(len(areas), "Na área", "Nas áreas"), joinNames(areas), when)
	}
	c.Padding = make([]struct{}, 90)

	var t strings.Builder
	fmt.Fprintf(&t, "%s na câmera %s, %s.\n\n", title, a.CameraName, when)
	for _, o := range a.Objects {
		fmt.Fprintf(&t, "- %s %s", familyName(o.Family), score(o.Score))
		where := "imagem inteira"
		if len(o.Areas) > 0 {
			fmt.Fprintf(&t, ", %s %s", plural(len(o.Areas), "na área", "nas áreas"), joinNames(o.Areas))
			where = plural(len(o.Areas), "área ", "áreas ") + joinNames(o.Areas)
		}
		t.WriteString("\n")
		c.Objects = append(c.Objects, emailObject{Name: familyName(o.Family), Score: score(o.Score),
			Where: where, Color: template.CSS(hexColor(familyColor(o.Family)))})
	}
	if publicURL != "" {
		link := recordingLink(publicURL, a.Camera, onset)
		c.Link = template.URL(link)
		fmt.Fprintf(&t, "\nVer na gravação:\n%s\n", link)
	} else {
		t.WriteString("\nVeja mais detalhes na tela de Gravações. Para aparecer aqui o link que leva " +
			"direto a este momento, peça ao administrador que defina server.publicUrl.\n")
	}

	m := email.Message{To: to, Subject: c.Subject, Text: t.String(),
		Attachments: []email.Attachment{{Name: "dwnvr.png", Type: "image/png", Data: logoPNG, InlineID: logoID}}}
	if len(a.Frame) > 0 {
		c.Image = frameID
		c.ImageAlt = fmt.Sprintf("Quadro da câmera %s às %s", a.CameraName, onset.Format("15:04:05"))
		m.Attachments = append(m.Attachments, email.Attachment{
			Name:     fmt.Sprintf("%s-%s.jpg", a.Camera, onset.Format("2006-01-02-150405")),
			Type:     "image/jpeg",
			Data:     a.Frame,
			InlineID: frameID,
		})
	}
	var h bytes.Buffer
	if err := alertTemplate.Execute(&h, c); err != nil {
		return email.Message{}, fmt.Errorf("building the alert HTML: %w", err)
	}
	m.HTML = h.String()
	return m, nil
}

// oneLine replaces each run of spaces, tabs and line breaks with a single
// space.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// recordingLink opens the Recordings screen on the camera and the instant,
// in the format of the web's linkGravacoes (web/src/lib/rota.svelte.ts). The
// day and the time are the server's, and the screen reads them in the
// browser's time zone: someone opening it from another time zone lands
// shifted.
func recordingLink(publicURL, camera string, at time.Time) string {
	return fmt.Sprintf("%s/#rec?cam=%s&day=%s&t=%s", publicURL, url.QueryEscape(camera),
		at.Format("2006-01-02"), at.Format("15:04:05"))
}

// familyNames are the names of the objects' families, in lowercase,
// without repeats, in the order they appear.
func familyNames(objects []Object) []string {
	var names []string
	for _, o := range objects {
		n := strings.ToLower(familyName(o.Family))
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	return names
}

func capitalize(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}

func familyColor(f string) color.RGBA {
	if c, ok := familyColors[detect.Family(f)]; ok {
		return c
	}
	return unknownColor
}

func hexColor(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func familyName(f string) string {
	if n, ok := familyDisplayNames[detect.Family(f)]; ok {
		return n
	}
	return f
}

// score writes the confidence as the screen does: "0,87".
func score(s float64) string {
	return strings.Replace(strconv.FormatFloat(s, 'f', 2, 64), ".", ",", 1)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// joinNames writes "Portão", "Portão e Calçada" or "Portão, Calçada e
// Garagem".
func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " e " + names[len(names)-1]
}
