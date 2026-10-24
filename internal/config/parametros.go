package config

import (
	"fmt"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// As faixas aceitas em cada campo numérico. São a régua única das duas portas
// por onde um valor entra: a API recusa o que cai fora delas, e a leitura dos
// arquivos no boot troca pelo padrão e avisa (ver Aviso). A tela de Câmeras as
// recebe pelo GET /api/cameras (FaixasDaCamera), e o min/max dos inputs sai
// daqui, sem número repetido no Cameras.svelte.
var (
	// FaixaQuotaMB: uma cota de poucos MB não guarda nem um segmento, e a
	// câmera passaria a vida apagando o que acabou de gravar.
	FaixaQuotaMB = Faixa{Min: 100, Unidade: "MB"}

	// FaixaSegmentSeconds: o segmento aberto é o que se perde numa queda de
	// energia, e é o arquivo que o player baixa inteiro a cada seek. Cinco
	// minutos já são uns 75 MB a 2 Mbps; o piso evita uma enxurrada de
	// arquivos pequenos no disco.
	FaixaSegmentSeconds = Faixa{Min: 10, Max: 300, Unidade: "s"}

	// FaixaStallSeconds: zero ou negativo desligaria a vigilância que impede a
	// câmera de parar de gravar em silêncio; acima de uma hora, também.
	FaixaStallSeconds = Faixa{Min: 1, Max: 3600, Unidade: "s"}

	// FaixaMaxDays: zero é "sem limite de idade, quem manda é a cota".
	FaixaMaxDays = Faixa{Min: 0, Unidade: "dias"}

	// FaixaMinFreeMB: zero desliga a trava global de disco; negativo não
	// significa nada.
	FaixaMinFreeMB = Faixa{Min: 0, Unidade: "MB"}

	// faixaSensibilidade é a dos níveis que o detect conhece; o número mora lá
	// porque cada nível tem um limiar medido.
	faixaSensibilidade = Faixa{Min: detect.NivelMin, Max: detect.NivelMax}

	// FaixaLoginAttemptsPerMinute: zero desliga o limite. Cada tentativa custa
	// uma conta de senha inteira (0,77 s de CPU num Orange Pi Zero 3). 12 por
	// minuto, uma a cada 5 s, já é muito para quem digita a senha.
	FaixaLoginAttemptsPerMinute = Faixa{Min: 0, Max: 12, Unidade: "por minuto"}

	// FaixaPortaSMTP é a das portas TCP.
	FaixaPortaSMTP = Faixa{Min: 1, Max: 65535}
)

// PortaSMTPPadrao é a porta de cada smtp.security, a que os provedores usam:
// 587 é a da submissão com STARTTLS (RFC 6409), 465 a do TLS direto
// (RFC 8314), e 25 a do SMTP sem TLS, de relay na rede local.
var PortaSMTPPadrao = map[string]int{"starttls": 587, "tls": 465, "none": 25}

// LoginAttemptsPerMinutePadrao: depois da rajada, uma tentativa a cada 30 s.
// Quem digitou errado espera pouco, e quem testa senhas fica em umas 2.900
// por dia por endereço, contra uma a cada 0,77 s sem limite.
const LoginAttemptsPerMinutePadrao = 2

// MaxHorariosDoAlerta é o que a grade da tela gera no pior caso: as horas
// marcadas uma sim, outra não dão 12 horários por dia, e com cada dia de um
// jeito são 84 na semana. Os 84, mesmo com os sete dias em cada um, ocupam
// uns 4,5 KB dos 16 KB que o POST /api/cameras lê, ao lado dos ~3 KB das oito
// áreas.
const MaxHorariosDoAlerta = 84

// TrustedProxiesPadrao são o loopback e a faixa que o Docker usa para as
// redes do compose. É por ela que chegam os dois caminhos de HTTPS da doc: o
// `tailscale serve`, pelo gateway da rede, e o Caddy do compose, pelo IP do
// container. Os dois substituem o X-Forwarded-For que o cliente mandar
// (conferido em 13/10/2026). O loopback cobre o dwnvr fora do Docker, com o
// proxy na mesma máquina.
var TrustedProxiesPadrao = []string{"127.0.0.0/8", "::1/128", "172.16.0.0/12"}

// FaixasDaCamera são as faixas dos campos numéricos da câmera, pelo nome no
// JSON. É o que a API manda à tela; campo numérico novo com input na tela
// entra aqui.
var FaixasDaCamera = map[string]Faixa{
	"quotaMB":             FaixaQuotaMB,
	"segmentSeconds":      FaixaSegmentSeconds,
	"maxDays":             FaixaMaxDays,
	"stallSeconds":        FaixaStallSeconds,
	"detectSensibilidade": faixaSensibilidade,
}

// Faixa é um intervalo fechado de valores aceitos. Max zero é "sem teto", e
// some do JSON para o input da tela ficar sem max.
type Faixa struct {
	Min     int64  `json:"min"`
	Max     int64  `json:"max,omitempty"`
	Unidade string `json:"-"`
}

// Contem diz se v está dentro da faixa.
func (f Faixa) Contem(v int64) bool {
	return v >= f.Min && (f.Max == 0 || v <= f.Max)
}

// String descreve a faixa como recomendação: "de 10 a 300 s", "100 MB ou mais".
func (f Faixa) String() string {
	un := ""
	if f.Unidade != "" {
		un = " " + f.Unidade
	}
	if f.Max == 0 {
		return fmt.Sprintf("%d%s ou mais", f.Min, un)
	}
	return fmt.Sprintf("de %d a %d%s", f.Min, f.Max, un)
}
