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
		{"com as duas", config.Config{SMTP: smtp, Alerts: config.Alerts{Email: config.AlertsEmail{To: para}},
			Server: config.Server{PublicURL: "https://dwnvr.exemplo.com"}}, true, nil},
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
		if e, ok := n.(*Email); ok && (!slices.Equal(e.para, para) || e.endereco != c.cfg.Server.PublicURL) {
			t.Errorf("%s: para %v e endereço %q", c.nome, e.para, e.endereco)
		}
	}
	if _, ok := Notificadores[MeioEmail]; !ok {
		t.Error("o email não está no mapa de construtores")
	}
}

// TestMensagemDoAlerta: uma mensagem só para a lista inteira; o assunto com
// a câmera, as áreas, as famílias sem repetir e a hora; o texto com o dia,
// cada objeto, as áreas e o link para a gravação; o HTML com o mesmo e o
// quadro no corpo.
func TestMensagemDoAlerta(t *testing.T) {
	instante := time.Date(2026, 10, 27, 14, 32, 5, 0, time.Local)
	a := Alerta{Camera: "cam garagem", NomeDaCamera: "Garagem", InstanteMs: instante.UnixMilli(),
		Quadro: []byte("jpeg"), Objetos: []Objeto{
			{Familia: "pessoa", Classe: "person", Score: 0.87, Areas: []string{"Portão"}},
			{Familia: "pessoa", Classe: "person", Score: 0.551, Areas: []string{"Portão", "Calçada", "Rampa"}},
			{Familia: "veiculo", Classe: "car", Score: 0.7},
		}}
	f := &enviadorFalso{}
	e := &Email{envio: f, para: []string{"fulano@exemplo.com", "ciclana@exemplo.com"},
		endereco: "https://dwnvr.exemplo.com"}
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
	if want := "[Alerta] Garagem(Portão, Calçada e Rampa): pessoa e veículo às 14:32"; m.Assunto != want {
		t.Errorf("assunto %q, esperado %q", m.Assunto, want)
	}
	// O espaço da câmera vira "+", que o URLSearchParams da tela lê de volta.
	link := "https://dwnvr.exemplo.com/#rec?cam=cam+garagem&day=2026-10-27&t=14:32:05"
	want := "Pessoa e veículo na câmera Garagem, terça-feira, 27/10/2026 às 14:32:05.\n\n" +
		"- Pessoa 0,87, na área Portão\n" +
		"- Pessoa 0,55, nas áreas Portão, Calçada e Rampa\n" +
		"- Veículo 0,70\n\n" +
		"Ver na gravação:\n" + link + "\n"
	if m.Texto != want {
		t.Errorf("corpo\n%s\nesperado\n%s", m.Texto, want)
	}
	if len(m.Anexos) != 2 || !ehAMarca(m.Anexos[0]) ||
		m.Anexos[1].Nome != "cam garagem-2026-10-27-143205.jpg" ||
		m.Anexos[1].Tipo != "image/jpeg" || string(m.Anexos[1].Dados) != "jpeg" ||
		m.Anexos[1].NoCorpo != idDoQuadro {
		t.Errorf("anexos %+v", m.Anexos)
	}
	for _, trecho := range []string{
		// A prévia da caixa de entrada.
		"Nas áreas Portão, Calçada e Rampa · terça-feira, 27/10/2026 às 14:32:05",
		`<img src="cid:` + idDoQuadro + `"`, `<img src="cid:` + idDaMarca + `"`,
		"Alerta · Garagem</td>", ">Alerta da câmera Garagem</div>",
		">Pessoa e veículo</div>",
		"Câmera Garagem · terça-feira, 27/10/2026\u00a0às\u00a014:32:05",
		"áreas Portão, Calçada e Rampa", "imagem inteira", "background:#a371f7",
		`href="https://dwnvr.exemplo.com/#rec?cam=cam&#43;garagem&amp;day=2026-10-27&amp;t=14:32:05"`,
	} {
		if !strings.Contains(m.HTML, trecho) {
			t.Errorf("o HTML não tem %q", trecho)
		}
	}
	if strings.Contains(m.HTML, "server.publicUrl") {
		t.Error("com o endereço, o HTML ainda pede o server.publicUrl")
	}
}

// TestMensagemSemEndereco: sem o server.publicUrl, nem o texto nem o HTML
// levam link, e os dois dizem a quem pedir.
func TestMensagemSemEndereco(t *testing.T) {
	m, err := mensagemDoAlerta(alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli()),
		[]string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for nome, corpo := range map[string]string{"texto": m.Texto, "HTML": m.HTML} {
		if strings.Contains(corpo, "#rec?") || !strings.Contains(corpo, "server.publicUrl") {
			t.Errorf("%s sem o endereço:\n%s", nome, corpo)
		}
	}
	if strings.Contains(m.HTML, "<a ") {
		t.Error("o HTML tem um link sem o endereço")
	}
}

// TestMensagemSemQuadro: o quadro que não foi gravado não vira anexo, e o
// HTML não o mostra; a marca do header vai do mesmo jeito.
func TestMensagemSemQuadro(t *testing.T) {
	a := alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Quadro = nil
	m, err := mensagemDoAlerta(a, []string{"fulano@exemplo.com"}, "https://dwnvr.exemplo.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Anexos) != 1 || !ehAMarca(m.Anexos[0]) || strings.Contains(m.HTML, "cid:"+idDoQuadro) {
		t.Errorf("sem quadro, anexos %+v e HTML\n%s", m.Anexos, m.HTML)
	}
	if m.Assunto != "[Alerta] Frente(Portão): pessoa às 03:00" {
		t.Errorf("assunto %q", m.Assunto)
	}
}

// TestAssuntoSemArea: o alerta que vale na imagem inteira sai sem os
// parênteses das áreas no assunto.
func TestAssuntoSemArea(t *testing.T) {
	a := alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Objetos[0].Areas = nil
	m, err := mensagemDoAlerta(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Alerta] Frente: pessoa às 03:00"; m.Assunto != want {
		t.Errorf("assunto %q, esperado %q", m.Assunto, want)
	}
}

// TestAssuntoEmUmaLinha: a quebra de linha no nome da área, escrito à mão no
// cameras.json, vira espaço, que o envio recusa o assunto com quebra.
func TestAssuntoEmUmaLinha(t *testing.T) {
	a := alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.Objetos[0].Areas = []string{"Portão\r\nprincipal"}
	m, err := mensagemDoAlerta(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Alerta] Frente(Portão principal): pessoa às 03:00"; m.Assunto != want {
		t.Errorf("assunto %q, esperado %q", m.Assunto, want)
	}
}

// TestMensagemEscapaONome: o nome da câmera e o da área são do usuário, e
// no HTML saem como texto.
func TestMensagemEscapaONome(t *testing.T) {
	a := alertaDe(time.Date(2026, 10, 27, 3, 0, 0, 0, time.Local).UnixMilli())
	a.NomeDaCamera = `Frente <script>"x"</script>`
	a.Objetos[0].Areas = []string{"<b>Portão</b>"}
	m, err := mensagemDoAlerta(a, []string{"fulano@exemplo.com"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "<b>") ||
		!strings.Contains(m.HTML, "Frente &lt;script&gt;") {
		t.Errorf("o HTML não escapou o nome:\n%s", m.HTML)
	}
}

// ehAMarca: a marca vai no corpo, como o PNG embutido.
func ehAMarca(a email.Anexo) bool {
	return a.Nome == "dwnvr.png" && a.Tipo == "image/png" && a.NoCorpo == idDaMarca &&
		len(a.Dados) > 0 && string(a.Dados[1:4]) == "PNG"
}
