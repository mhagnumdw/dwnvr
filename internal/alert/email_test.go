package alert

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/email"
)

// fakeSender keeps the ready message, which is what the notifier delivers.
// The real send, with the attachment, is tested in internal/email against a
// fake SMTP server.
type fakeSender struct{ received []email.Message }

func (f *fakeSender) Send(_ context.Context, m email.Message) error {
	f.received = append(f.received, m)
	return nil
}

// TestEmailNeedsBothThings: without the smtp section on or without an
// address in the list, the notifier isn't built, and the alert stays only in
// the record; MissingForEmail says what's missing, and is only empty when the
// notifier gets built.
func TestEmailNeedsBothThings(t *testing.T) {
	smtp := config.SMTP{Host: "smtp.exemplo.com", Port: 587, Security: "starttls", From: "dwnvr@exemplo.com"}
	to := []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	for _, c := range []struct {
		name    string
		cfg     config.Config
		born    bool
		missing []string
	}{
		{"nothing", config.Config{}, false, []string{"alerts.email.to", "smtp"}},
		{"without the list", config.Config{SMTP: smtp}, false, []string{"alerts.email.to"}},
		{"without smtp", config.Config{Alerts: config.Alerts{Email: config.AlertsEmail{To: to}}}, false,
			[]string{"smtp"}},
		{"smtp rejected", config.Config{SMTP: config.SMTP{Host: "smtp.exemplo.com", Problem: "smtp.from vazio"},
			Alerts: config.Alerts{Email: config.AlertsEmail{To: to}}}, false,
			[]string{"smtp"}},
		{"smtp rejected and without the list", config.Config{SMTP: config.SMTP{Problem: "smtp.host vazio"}}, false,
			[]string{"alerts.email.to", "smtp"}},
		{"with both", config.Config{SMTP: smtp, Alerts: config.Alerts{Email: config.AlertsEmail{To: to}},
			Server: config.Server{PublicURL: "https://dwnvr.exemplo.com"}}, true, nil},
	} {
		if got := MissingForEmail(&c.cfg); !slices.Equal(got, c.missing) {
			t.Errorf("%s: missing %q, expected %q", c.name, got, c.missing)
		}
		n, err := NewEmail(&c.cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if (n != nil) != c.born {
			t.Errorf("%s: built %v, expected %v", c.name, n != nil, c.born)
		}
		if e, ok := n.(*Email); ok && (!slices.Equal(e.to, to) || e.publicURL != c.cfg.Server.PublicURL) {
			t.Errorf("%s: to %v and address %q", c.name, e.to, e.publicURL)
		}
	}
	if _, ok := Notifiers[NotifierEmail]; !ok {
		t.Error("email isn't in the constructor map")
	}
}

// TestAlertMessage: a single message to the whole list; the subject with
// the camera, the areas, the families without repeats and the time; the text
// with the day, each object, the areas and the link to the recording; the HTML
// with the same and the frame in the body.
func TestAlertMessage(t *testing.T) {
	at := time.Date(2026, 10, 27, 14, 32, 5, 0, time.Local)
	a := Alert{Camera: "cam garagem", CameraName: "Garagem", AtMs: at.UnixMilli(),
		Frame: []byte("jpeg"), Objects: []Object{
			{Family: "pessoa", Class: "person", Score: 0.87, Areas: []string{"Portão"}},
			{Family: "pessoa", Class: "person", Score: 0.551, Areas: []string{"Portão", "Calçada", "Rampa"}},
			{Family: "veiculo", Class: "car", Score: 0.7},
		}}
	f := &fakeSender{}
	e := &Email{sender: f, to: []string{"fulano@exemplo.com", "ciclana@exemplo.com"},
		publicURL: "https://dwnvr.exemplo.com"}
	if err := e.Send(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if len(f.received) != 1 {
		t.Fatalf("%d messages, want 1 for the whole list", len(f.received))
	}
	m := f.received[0]
	if !slices.Equal(m.To, e.to) {
		t.Errorf("to %v", m.To)
	}
	if want := "[Alerta] Garagem(Portão, Calçada e Rampa): pessoa e veículo às 14:32"; m.Subject != want {
		t.Errorf("subject %q, expected %q", m.Subject, want)
	}
	// The space in the camera becomes "+", which the screen's URLSearchParams reads back.
	link := "https://dwnvr.exemplo.com/#rec?cam=cam+garagem&day=2026-10-27&t=14:32:05"
	want := "Pessoa e veículo na câmera Garagem, terça-feira, 27/10/2026 às 14:32:05.\n\n" +
		"- Pessoa 0,87, na área Portão\n" +
		"- Pessoa 0,55, nas áreas Portão, Calçada e Rampa\n" +
		"- Veículo 0,70\n\n" +
		"Ver na gravação:\n" + link + "\n"
	if m.Text != want {
		t.Errorf("body\n%s\nwant\n%s", m.Text, want)
	}
	if len(m.Attachments) != 2 || !isLogo(m.Attachments[0]) ||
		m.Attachments[1].Name != "cam garagem-2026-10-27-143205.jpg" ||
		m.Attachments[1].Type != "image/jpeg" || string(m.Attachments[1].Data) != "jpeg" ||
		m.Attachments[1].InlineID != frameID {
		t.Errorf("attachments %+v", m.Attachments)
	}
	for _, snippet := range []string{
		// The inbox preview.
		"Nas áreas Portão, Calçada e Rampa · terça-feira, 27/10/2026 às 14:32:05",
		`<img src="cid:` + frameID + `"`, `<img src="cid:` + logoID + `"`,
		"Alerta · Garagem</td>", ">Alerta da câmera Garagem</div>",
		">Pessoa e veículo</div>",
		"Câmera Garagem · terça-feira, 27/10/2026\u00a0às\u00a014:32:05",
		"áreas Portão, Calçada e Rampa", "imagem inteira", "background:#a371f7",
		`href="https://dwnvr.exemplo.com/#rec?cam=cam&#43;garagem&amp;day=2026-10-27&amp;t=14:32:05"`,
	} {
		if !strings.Contains(m.HTML, snippet) {
			t.Errorf("the HTML doesn't have %q", snippet)
		}
	}
	if strings.Contains(m.HTML, "server.publicUrl") {
		t.Error("with the address, the HTML still asks for server.publicUrl")
	}
}

// TestMessageWithoutAddress: without server.publicUrl, neither the text nor
// the HTML carry a link, and both say who to ask.
func TestMessageWithoutAddress(t *testing.T) {
	m, err := alertMessage(alertOf(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli()),
		[]string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"texto": m.Text, "HTML": m.HTML} {
		if strings.Contains(body, "#rec?") || !strings.Contains(body, "server.publicUrl") {
			t.Errorf("%s without the address:\n%s", name, body)
		}
	}
	if strings.Contains(m.HTML, "<a ") {
		t.Error("the HTML has a link without the address")
	}
}

// TestMessageWithoutFrame: a frame that wasn't stored doesn't become an
// attachment, and the HTML doesn't show it; the header logo goes all the
// same.
func TestMessageWithoutFrame(t *testing.T) {
	a := alertOf(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Frame = nil
	m, err := alertMessage(a, []string{"fulano@exemplo.com"}, "https://dwnvr.exemplo.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 || !isLogo(m.Attachments[0]) || strings.Contains(m.HTML, "cid:"+frameID) {
		t.Errorf("without a frame, attachments %+v and HTML\n%s", m.Attachments, m.HTML)
	}
	if m.Subject != "[Alerta] Frente(Portão): pessoa às 03:00" {
		t.Errorf("subject %q", m.Subject)
	}
}

// TestSubjectWithoutArea: an alert that applies to the whole image goes out
// without the areas' parentheses in the subject.
func TestSubjectWithoutArea(t *testing.T) {
	a := alertOf(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Objects[0].Areas = nil
	m, err := alertMessage(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Alerta] Frente: pessoa às 03:00"; m.Subject != want {
		t.Errorf("subject %q, expected %q", m.Subject, want)
	}
}

// TestSubjectOnOneLine: a line break in the area name, written by hand in
// cameras.json, becomes a space, since sending rejects a subject with a line
// break.
func TestSubjectOnOneLine(t *testing.T) {
	a := alertOf(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Objects[0].Areas = []string{"Portão\r\nprincipal"}
	m, err := alertMessage(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Alerta] Frente(Portão principal): pessoa às 03:00"; m.Subject != want {
		t.Errorf("subject %q, expected %q", m.Subject, want)
	}
}

// TestMessageEscapesTheName: the camera name and the area name come from
// the user, and in the HTML they come out as text.
func TestMessageEscapesTheName(t *testing.T) {
	a := alertOf(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.CameraName = `Frente <script>"x"</script>`
	a.Objects[0].Areas = []string{"<b>Portão</b>"}
	m, err := alertMessage(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "<b>") ||
		!strings.Contains(m.HTML, "Frente &lt;script&gt;") {
		t.Errorf("the HTML didn't escape the name:\n%s", m.HTML)
	}
}

// isLogo: the logo goes in the body, as the embedded PNG.
func isLogo(a email.Attachment) bool {
	return a.Name == "dwnvr.png" && a.Type == "image/png" && a.InlineID == logoID &&
		len(a.Data) > 0 && string(a.Data[1:4]) == "PNG"
}
