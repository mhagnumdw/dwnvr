package alerta

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

// Email é o meio que manda o alerta por e-mail: pela seção smtp do dwnvr.yaml,
// a mesma do link de definir a senha, para a lista alerts.email.to, numa
// mensagem só para todos.
type Email struct {
	envio email.Sender
	para  []string
	// endereco é o server.publicUrl, base do link para a gravação; vazio, o
	// e-mail sai sem o botão.
	endereco string
}

// NovoEmail monta o meio com a seção smtp ligada e pelo menos um endereço na
// lista; sem uma das duas, devolve nil, e o alerta fica só no registro. A
// lista já vem conferida pelo config.Load.
func NovoEmail(cfg *config.Config) (Notificador, error) {
	if !cfg.SMTP.Enabled() || len(cfg.Alerts.Email.To) == 0 {
		return nil, nil
	}
	smtp, err := email.NewSMTP(cfg.SMTP)
	if err != nil {
		return nil, err
	}
	return &Email{envio: smtp, para: cfg.Alerts.Email.To, endereco: cfg.Server.PublicURL}, nil
}

// MeioEmail é o nome do meio de e-mail, no Notificadores e no estado dele.
const MeioEmail = "email"

func (e *Email) Nome() string { return MeioEmail }

// FaltaParaOEmail diz as seções do dwnvr.yaml que faltam para o alerta sair
// por e-mail, na ordem "alerts.email.to", "smtp", e nada quando o meio nasce.
// A seção smtp recusada conta como faltando. As regras são as do NovoEmail.
func FaltaParaOEmail(cfg *config.Config) []string {
	var falta []string
	if len(cfg.Alerts.Email.To) == 0 {
		falta = append(falta, "alerts.email.to")
	}
	if !cfg.SMTP.Enabled() {
		falta = append(falta, "smtp")
	}
	return falta
}

// Envia monta a mensagem do alerta e a entrega ao servidor, dentro do prazo
// do ctx e do email.SendTimeout, o que vencer primeiro.
func (e *Email) Envia(ctx context.Context, a Alerta) error {
	m, err := mensagemDoAlerta(a, e.para, e.endereco)
	if err != nil {
		return err
	}
	return e.envio.Send(ctx, m)
}

// nomesDasFamilias são os nomes que a mensagem mostra, os mesmos da tela.
var nomesDasFamilias = map[detect.Family]string{
	detect.Person:  "Pessoa",
	detect.Vehicle: "Veículo",
	detect.Animal:  "Animal",
}

var diasDaSemana = [...]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira",
	"quinta-feira", "sexta-feira", "sábado"}

//go:embed email.html
var htmlDoAlerta string

var modeloDoAlerta = template.Must(template.New("email.html").Parse(htmlDoAlerta))

// pngDaMarca é o ícone do dwnvr no header do e-mail, em PNG de 48 px para a tela
// de densidade 2: o Gmail e o Outlook não mostram SVG. Sai do
// web/public/icon-192.png, que é o favicon.svg em PNG:
//
//	magick web/public/icon-192.png -resize 48x48 -strip internal/alerta/marca.png
//
//go:embed marca.png
var pngDaMarca []byte

// idDoQuadro e idDaMarca são os cid: pelos quais o HTML mostra o quadro e a
// marca.
const (
	idDoQuadro = "quadro@dwnvr"
	idDaMarca  = "marca@dwnvr"
)

// corpoDoAlerta é o que o email.html recebe. Todo texto chega pronto, e o
// html/template o escapa: o nome da câmera e o das áreas são do usuário.
type corpoDoAlerta struct {
	Assunto, Previa, Titulo, Linha, Rodape, Camera string
	// Enchimento só repete um caractere invisível depois da prévia, para o
	// leitor de e-mail não completar a prévia com o começo do corpo.
	Enchimento []struct{}
	Objetos    []objetoNoEmail
	// Marca é o cid do ícone do header; Imagem, o do quadro, vazio sem quadro.
	Marca, Imagem, AltDaImagem string
	// Link abre a gravação no instante; vazio sem o server.publicUrl.
	Link template.URL
}

type objetoNoEmail struct {
	Nome, Score, Onde string
	Cor               template.CSS
}

// mensagemDoAlerta escreve o e-mail de um alerta, em HTML e em texto. O
// assunto diz o que importa num relance, como "[Alerta] Garagem(Portão):
// pessoa e veículo às 14:32", com as áreas em que algum objeto caiu entre
// parênteses, sem eles quando o alerta vale na imagem inteira, e as famílias
// na ordem de prioridade, que é a dos objetos (o Recorder os ordena). O corpo
// traz a marca do dwnvr no topo, o quadro, o dia, cada objeto e as áreas em
// que caiu, e o botão que abre a gravação no instante, quando o
// server.publicUrl diz o endereço do dwnvr.
func mensagemDoAlerta(a Alerta, para []string, endereco string) (email.Message, error) {
	// A hora é a do servidor, a mesma do dia das gravações e dos horários do
	// alerta.
	onset := time.UnixMilli(a.InstanteMs).In(time.Local)
	quando := fmt.Sprintf("%s, %s às %s", diasDaSemana[onset.Weekday()],
		onset.Format("02/01/2006"), onset.Format("15:04:05"))
	familias := familiasNoEmail(a.Objetos)
	titulo := maiuscula(juntaNomes(familias))
	areas := areasDe(a)
	ondeNoAssunto := a.NomeDaCamera
	if len(areas) > 0 {
		ondeNoAssunto += "(" + juntaNomes(areas) + ")"
	}

	c := corpoDoAlerta{
		// O assunto é uma linha só, que o envio recusa o assunto com quebra: a
		// que vier num nome escrito à mão no cameras.json vira espaço.
		Assunto: emUmaLinha(fmt.Sprintf("[Alerta] %s: %s às %s", ondeNoAssunto, juntaNomes(familias),
			onset.Format("15:04"))),
		Titulo: titulo,
		// No HTML a data e a hora não se separam na quebra de linha do celular.
		Linha: fmt.Sprintf("Câmera %s · %s", a.NomeDaCamera,
			strings.Replace(quando, " às ", "\u00a0às\u00a0", 1)),
		Rodape: "Alerta da câmera " + a.NomeDaCamera,
		Previa: quando,
		Camera: a.NomeDaCamera,
		Marca:  idDaMarca,
	}
	if len(areas) > 0 {
		c.Previa = fmt.Sprintf("%s %s · %s", plural(len(areas), "Na área", "Nas áreas"), juntaNomes(areas), quando)
	}
	c.Enchimento = make([]struct{}, 90)

	var t strings.Builder
	fmt.Fprintf(&t, "%s na câmera %s, %s.\n\n", titulo, a.NomeDaCamera, quando)
	for _, o := range a.Objetos {
		fmt.Fprintf(&t, "- %s %s", nomeDaFamilia(o.Familia), score(o.Score))
		onde := "imagem inteira"
		if len(o.Areas) > 0 {
			fmt.Fprintf(&t, ", %s %s", plural(len(o.Areas), "na área", "nas áreas"), juntaNomes(o.Areas))
			onde = plural(len(o.Areas), "área ", "áreas ") + juntaNomes(o.Areas)
		}
		t.WriteString("\n")
		c.Objetos = append(c.Objetos, objetoNoEmail{Nome: nomeDaFamilia(o.Familia), Score: score(o.Score),
			Onde: onde, Cor: template.CSS(hexa(corDaFamilia(o.Familia)))})
	}
	if endereco != "" {
		link := linkDaGravacao(endereco, a.Camera, onset)
		c.Link = template.URL(link)
		fmt.Fprintf(&t, "\nVer na gravação:\n%s\n", link)
	} else {
		t.WriteString("\nVeja mais detalhes na tela de Gravações. Para aparecer aqui o link que leva " +
			"direto a este momento, peça ao administrador que defina server.publicUrl.\n")
	}

	m := email.Message{To: para, Subject: c.Assunto, Text: t.String(),
		Attachments: []email.Attachment{{Name: "dwnvr.png", Type: "image/png", Data: pngDaMarca, InlineID: idDaMarca}}}
	if len(a.Quadro) > 0 {
		c.Imagem = idDoQuadro
		c.AltDaImagem = fmt.Sprintf("Quadro da câmera %s às %s", a.NomeDaCamera, onset.Format("15:04:05"))
		m.Attachments = append(m.Attachments, email.Attachment{
			Name:     fmt.Sprintf("%s-%s.jpg", a.Camera, onset.Format("2006-01-02-150405")),
			Type:     "image/jpeg",
			Data:     a.Quadro,
			InlineID: idDoQuadro,
		})
	}
	var h bytes.Buffer
	if err := modeloDoAlerta.Execute(&h, c); err != nil {
		return email.Message{}, fmt.Errorf("montando o HTML do alerta: %w", err)
	}
	m.HTML = h.String()
	return m, nil
}

// emUmaLinha troca cada sequência de espaços, tabulações e quebras de linha
// por um espaço só.
func emUmaLinha(s string) string { return strings.Join(strings.Fields(s), " ") }

// linkDaGravacao abre a tela Gravações na câmera e no instante, no formato do
// linkGravacoes da web (web/src/lib/rota.svelte.ts). O dia e a hora são os do
// servidor, e a tela os lê no fuso do navegador: quem abre de outro fuso cai
// deslocado.
func linkDaGravacao(endereco, camera string, instante time.Time) string {
	return fmt.Sprintf("%s/#rec?cam=%s&day=%s&t=%s", endereco, url.QueryEscape(camera),
		instante.Format("2006-01-02"), instante.Format("15:04:05"))
}

// familiasNoEmail são os nomes das famílias dos objetos, em minúscula, sem
// repetir, na ordem em que aparecem.
func familiasNoEmail(objetos []Objeto) []string {
	var nomes []string
	for _, o := range objetos {
		n := strings.ToLower(nomeDaFamilia(o.Familia))
		if !slices.Contains(nomes, n) {
			nomes = append(nomes, n)
		}
	}
	return nomes
}

func maiuscula(s string) string {
	for i, r := range s {
		return strings.ToUpper(string(r)) + s[i+len(string(r)):]
	}
	return s
}

func corDaFamilia(f string) color.RGBA {
	if c, ok := coresDasFamilias[detect.Family(f)]; ok {
		return c
	}
	return corDesconhecida
}

func hexa(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func nomeDaFamilia(f string) string {
	if n, ok := nomesDasFamilias[detect.Family(f)]; ok {
		return n
	}
	return f
}

// score escreve a confiança como a tela: "0,87".
func score(s float64) string {
	return strings.Replace(strconv.FormatFloat(s, 'f', 2, 64), ".", ",", 1)
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return um
	}
	return varios
}

// juntaNomes escreve "Portão", "Portão e Calçada" ou "Portão, Calçada e
// Garagem".
func juntaNomes(nomes []string) string {
	if len(nomes) == 1 {
		return nomes[0]
	}
	return strings.Join(nomes[:len(nomes)-1], ", ") + " e " + nomes[len(nomes)-1]
}
