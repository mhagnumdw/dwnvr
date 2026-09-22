package store

import (
	"os"
	"testing"
)

func appendObjeto(t *testing.T, c *Camera, instanteMs int64, familia string) {
	t.Helper()
	if err := c.AppendEvento(Evento{InstanteMs: instanteMs, Familia: familia, Classe: "x", Score: 0.9}); err != nil {
		t.Fatal(err)
	}
}

func instantes(evs []Evento) []int64 {
	out := make([]int64, len(evs))
	for i, ev := range evs {
		out[i] = ev.InstanteMs
	}
	return out
}

// Só as marcas de objeto, em ordem de instante - mesmo que o detector tenha
// respondido fora de ordem.
func TestObjetosDoDiaSoAsMarcasEmOrdem(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	dia := baseTime().Format(DayLayout)
	c.AppendEvento(Evento{InstanteMs: base})
	appendObjeto(t, c, base+2000, "pessoa")
	c.AppendEvento(Evento{InstanteMs: base + 3000})
	appendObjeto(t, c, base+1000, "veiculo")

	got, err := c.ObjetosDoDia(dia)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{base + 1000, base + 2000}; len(got) != 2 || got[0].InstanteMs != want[0] || got[1].InstanteMs != want[1] {
		t.Errorf("instantes %v, esperado %v", instantes(got), want)
	}

	vazio, err := c.ObjetosDoDia("2020-01-01")
	if err != nil || len(vazio) != 0 {
		t.Errorf("dia sem arquivo: %d marcas, erro %v", len(vazio), err)
	}
}

// O dia corrente cresce: a leitura seguinte pega o que entrou, e a fatia que
// já tinha sido entregue não muda debaixo de quem a recebeu.
func TestObjetosDoDiaAcompanhaOArquivoCrescendo(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	dia := baseTime().Format(DayLayout)
	appendObjeto(t, c, base+2000, "pessoa")

	antes, _ := c.ObjetosDoDia(dia)
	appendObjeto(t, c, base+1000, "animal")
	c.AppendEvento(Evento{InstanteMs: base + 5000})

	depois, err := c.ObjetosDoDia(dia)
	if err != nil {
		t.Fatal(err)
	}
	if len(depois) != 2 || depois[0].InstanteMs != base+1000 {
		t.Errorf("depois de crescer: %v", instantes(depois))
	}
	if len(antes) != 1 || antes[0].InstanteMs != base+2000 {
		t.Errorf("a fatia entregue antes mudou: %v", instantes(antes))
	}
}

// A linha que o recorder ainda está escrevendo não entra, e não se perde: ela
// é lida inteira quando terminar.
func TestObjetosDoDiaEsperaALinhaTerminar(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	dia := baseTime().Format(DayLayout)
	appendObjeto(t, c, base, "pessoa")

	f, err := os.OpenFile(c.EventosPath(dia), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(`{"instanteMs":1786201260000,"familia":"vei`)

	got, _ := c.ObjetosDoDia(dia)
	if len(got) != 1 {
		t.Fatalf("a linha pela metade entrou: %v", instantes(got))
	}

	f.WriteString(`culo","classe":"car","score":0.8}` + "\n")
	got, _ = c.ObjetosDoDia(dia)
	if len(got) != 2 || got[1].Familia != "veiculo" {
		t.Errorf("a linha terminada não entrou inteira: %+v", got)
	}
}

// A retenção regrava o arquivo com rename (aparaEventos), e ele pode até
// crescer de novo além do tamanho antigo: o que estava memorizado não vale.
func TestObjetosDoDiaPercebeARegravacao(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	dia := baseTime().Format(DayLayout)
	appendObjeto(t, c, base, "pessoa")
	appendObjeto(t, c, base+60_000, "pessoa")
	c.ObjetosDoDia(dia)

	if err := c.aparaEventos(dia, base+1); err != nil {
		t.Fatal(err)
	}
	for i := int64(2); i < 6; i++ {
		appendObjeto(t, c, base+i*60_000, "animal")
	}

	got, err := c.ObjetosDoDia(dia)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].InstanteMs != base+60_000 {
		t.Errorf("depois da regravação: %v", instantes(got))
	}

	if err := os.Remove(c.EventosPath(dia)); err != nil { // a retenção levou o dia
		t.Fatal(err)
	}
	if got, _ := c.ObjetosDoDia(dia); len(got) != 0 {
		t.Errorf("o dia apagado continuou na memória: %v", instantes(got))
	}
}
