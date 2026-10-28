package alerta

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/email"
)

// enviadorFalso guarda a mensagem pronta, que é o que o meio entrega. O envio
// de verdade, com o anexo, é testado no internal/email contra um servidor
// SMTP falso.
type enviadorFalso struct{ recebidas []email.Mensagem }

func (f *enviadorFalso) Enviar(_ context.Context, m email.Mensagem) error {
	f.recebidas = append(f.recebidas, m)
	return nil
}

// TestEmailNasceComAsDuasCoisas: sem a seção smtp ligada ou sem endereço na
// lista, o meio não nasce, e o alerta fica só no registro; o FaltaParaOEmail
// diz o que falta, e só fica vazio quando o meio nasce.
func TestEmailNasceComAsDuasCoisas(t *testing.T) {
	smtp := config.SMTP{Host: "smtp.exemplo.com", Port: 587, Security: "starttls", From: "dwnvr@exemplo.com"}
	para := []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	for _, c := range []struct {
		nome  string
		cfg   config.Config
		nasce bool
		falta []string
	}{
		{"sem nada", config.Config{}, false, []string{"alerts.email.to", "smtp"}},
		{"sem a lista", config.Config{SMTP: smtp}, false, []string{"alerts.email.to"}},
		{"sem smtp", config.Config{Alerts: config.Alerts{Email: config.AlertsEmail{To: para}}}, false,
			[]string{"smtp"}},
		{"smtp recusado", config.Config{SMTP: config.SMTP{Host: "smtp.exemplo.com", Recusa: "smtp.from vazio"},
			Alerts: config.Alerts{Email: config.AlertsEmail{To: para}}}, false,
			[]string{"smtp"}},
		{"smtp recusado e sem a lista", config.Config{SMTP: config.SMTP{Recusa: "smtp.host vazio"}}, false,
			[]string{"alerts.email.to", "smtp"}},
		{"com as duas", config.Config{SMTP: smtp, Alerts: config.Alerts{Email: config.AlertsEmail{To: para}}},
			true, nil},
	} {
		if got := FaltaParaOEmail(&c.cfg); !slices.Equal(got, c.falta) {
			t.Errorf("%s: falta %q, esperado %q", c.nome, got, c.falta)
		}
		n, err := NovoEmail(&c.cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.nome, err)
		}
		if (n != nil) != c.nasce {
			t.Errorf("%s: nasceu %v, esperado %v", c.nome, n != nil, c.nasce)
		}
		if e, ok := n.(*Email); ok && !slices.Equal(e.para, para) {
			t.Errorf("%s: para %v", c.nome, e.para)
		}
	}
	if _, ok := Notificadores[MeioEmail]; !ok {
		t.Error("o email não está no mapa de construtores")
	}
}

// TestMensagemDoAlerta: uma mensagem só para a lista inteira; o assunto com a
// família de maior prioridade, a câmera e a hora; o corpo com o dia, cada
// objeto e as áreas; e o quadro anexo.
func TestMensagemDoAlerta(t *testing.T) {
	instante := time.Date(2026, 10, 27, 14, 32, 5, 0, time.Local)
	a := Alerta{Camera: "cam_garagem", NomeDaCamera: "Garagem", InstanteMs: instante.UnixMilli(),
		Quadro: []byte("jpeg"), Objetos: []Objeto{
			{Familia: "pessoa", Classe: "person", Score: 0.87, Areas: []string{"Portão"}},
			{Familia: "pessoa", Classe: "person", Score: 0.551, Areas: []string{"Portão", "Calçada", "Rampa"}},
			{Familia: "veiculo", Classe: "car", Score: 0.7},
		}}
	f := &enviadorFalso{}
	e := &Email{envio: f, para: []string{"fulano@exemplo.com", "ciclana@exemplo.com"}}
	if err := e.Envia(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if len(f.recebidas) != 1 {
		t.Fatalf("%d mensagens, esperada 1 para a lista inteira", len(f.recebidas))
	}
	m := f.recebidas[0]
	if !slices.Equal(m.Para, e.para) {
		t.Errorf("para %v", m.Para)
	}
	if want := "Pessoa em Garagem às 14:32"; m.Assunto != want {
		t.Errorf("assunto %q, esperado %q", m.Assunto, want)
	}
	want := "Pessoa em Garagem, terça-feira, 27/10/2026, às 14:32:05.\n\n" +
		"- Pessoa 0,87, na área Portão\n" +
		"- Pessoa 0,55, nas áreas Portão, Calçada e Rampa\n" +
		"- Veículo 0,70\n\n" +
		"O quadro olhado vai anexo.\n\n" +
		"O alerta avisa do que a detecção marcar, e ela não pega toda chegada: não é alarme.\n"
	if m.Texto != want {
		t.Errorf("corpo\n%s\nesperado\n%s", m.Texto, want)
	}
	if len(m.Anexos) != 1 || m.Anexos[0].Nome != "cam_garagem-2026-10-27-143205.jpg" ||
		m.Anexos[0].Tipo != "image/jpeg" || string(m.Anexos[0].Dados) != "jpeg" {
		t.Errorf("anexos %+v", m.Anexos)
	}
}

// TestMensagemSemQuadro: o quadro que não foi gravado não vira anexo, e o
// corpo não promete um.
func TestMensagemSemQuadro(t *testing.T) {
	a := alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Quadro = nil
	m := mensagemDoAlerta(a, []string{"fulano@exemplo.com"})
	if len(m.Anexos) != 0 || strings.Contains(m.Texto, "anexo") {
		t.Errorf("sem quadro, anexos %+v e corpo\n%s", m.Anexos, m.Texto)
	}
	if m.Assunto != "Pessoa em Frente às 03:00" {
		t.Errorf("assunto %q", m.Assunto)
	}
}
