package alerta

import (
	"context"
	"fmt"
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
	envio email.Enviador
	para  []string
}

// NovoEmail monta o meio com a seção smtp ligada e pelo menos um endereço na
// lista; sem uma das duas, devolve nil, e o alerta fica só no registro. A
// lista já vem conferida pelo config.Load.
func NovoEmail(cfg *config.Config) (Notificador, error) {
	if !cfg.SMTP.Ligado() || len(cfg.Alerts.Email.To) == 0 {
		return nil, nil
	}
	smtp, err := email.NovoSMTP(cfg.SMTP)
	if err != nil {
		return nil, err
	}
	return &Email{envio: smtp, para: cfg.Alerts.Email.To}, nil
}

func (e *Email) Nome() string { return "email" }

// Envia monta a mensagem do alerta e a entrega ao servidor, dentro do prazo
// do ctx e do email.PrazoDoEnvio, o que vencer primeiro.
func (e *Email) Envia(ctx context.Context, a Alerta) error {
	return e.envio.Enviar(ctx, mensagemDoAlerta(a, e.para))
}

// nomesDasFamilias são os nomes que a mensagem mostra, os mesmos da tela.
var nomesDasFamilias = map[detect.Familia]string{
	detect.Pessoa:  "Pessoa",
	detect.Veiculo: "Veículo",
	detect.Animal:  "Animal",
}

var diasDaSemana = [...]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira",
	"quinta-feira", "sexta-feira", "sábado"}

// mensagemDoAlerta escreve o e-mail de um alerta. O assunto diz o que importa
// num relance, como "Pessoa em Garagem às 14:32", com a família de maior
// prioridade, que é a do primeiro objeto (o Recorder os ordena). O corpo diz
// o dia, cada objeto e as áreas em que caiu, e o quadro olhado vai anexo.
//
// Sem link para abrir a detecção: o servidor não sabe o endereço pelo qual é
// acessado.
func mensagemDoAlerta(a Alerta, para []string) email.Mensagem {
	// A hora é a do servidor, a mesma do dia das gravações e dos horários do
	// alerta.
	onset := time.UnixMilli(a.InstanteMs).In(time.Local)
	familia := nomeDaFamilia(a.Objetos[0].Familia)

	var t strings.Builder
	fmt.Fprintf(&t, "%s em %s, %s, %s, às %s.\n\n", familia, a.NomeDaCamera,
		diasDaSemana[onset.Weekday()], onset.Format("02/01/2006"), onset.Format("15:04:05"))
	for _, o := range a.Objetos {
		fmt.Fprintf(&t, "- %s %s", nomeDaFamilia(o.Familia), score(o.Score))
		if len(o.Areas) > 0 {
			fmt.Fprintf(&t, ", %s %s", plural(len(o.Areas), "na área", "nas áreas"), juntaNomes(o.Areas))
		}
		t.WriteString("\n")
	}
	m := email.Mensagem{
		Para:    para,
		Assunto: fmt.Sprintf("%s em %s às %s", familia, a.NomeDaCamera, onset.Format("15:04")),
	}
	if len(a.Quadro) > 0 {
		t.WriteString("\nO quadro olhado vai anexo.\n")
		m.Anexos = []email.Anexo{{
			Nome:  fmt.Sprintf("%s-%s.jpg", a.Camera, onset.Format("2006-01-02-150405")),
			Tipo:  "image/jpeg",
			Dados: a.Quadro,
		}}
	}
	t.WriteString("\nO alerta avisa do que a detecção marcar, e ela não pega toda chegada: não é alarme.\n")
	m.Texto = t.String()
	return m
}

func nomeDaFamilia(f string) string {
	if n, ok := nomesDasFamilias[detect.Familia(f)]; ok {
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
