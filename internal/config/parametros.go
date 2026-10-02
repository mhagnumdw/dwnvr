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
)

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
