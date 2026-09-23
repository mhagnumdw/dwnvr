// Package logbuf guarda em memória os últimos avisos e erros do log, para a
// tela de Diagnóstico mostrar.
//
// Existe porque o `docker logs` é o primeiro passo de qualquer investigação e
// também o que quem instalou o dwnvr menos sabe fazer. Com as últimas linhas
// na tela, a pessoa copia e cola numa conversa em vez de abrir um terminal.
//
// Só WARN e ERROR entram: o INFO do dwnvr é o dia a dia (índice carregado,
// dwnvr no ar), e deixá-lo entrar faria um reinício empurrar para fora
// justamente o erro que explicava o reinício.
package logbuf

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Linha é um aviso ou erro já formatado: a mensagem seguida dos atributos em
// chave=valor, como o TextHandler escreveria.
type Linha struct {
	Em    time.Time `json:"em"`
	Nivel string    `json:"nivel"`
	Texto string    `json:"texto"`
}

// anel é compartilhado por todos os Handler derivados de um mesmo New: o
// log.With("cam", ...) de cada recorder grava no mesmo lugar que a raiz.
type anel struct {
	mu     sync.Mutex
	linhas []Linha
	prox   int
	cheio  bool
	total  int64
}

func (a *anel) guardar(l Linha) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.linhas[a.prox] = l
	a.prox = (a.prox + 1) % len(a.linhas)
	if a.prox == 0 {
		a.cheio = true
	}
	a.total++
}

// Handler embrulha outro slog.Handler: tudo segue para ele como antes, e o que
// for WARN ou acima fica também guardado no anel.
type Handler struct {
	prox  slog.Handler
	anel  *anel
	attrs string // atributos de With, já formatados
	grupo string // prefixo de WithGroup, com o ponto
}

// New guarda as últimas n linhas de aviso ou erro.
func New(prox slog.Handler, n int) *Handler {
	return &Handler{prox: prox, anel: &anel{linhas: make([]Linha, n)}}
}

// Enabled deixa passar todo aviso e erro, mesmo que o handler de baixo esteja
// num nível acima: o anel guarda o que ele descartaria.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelWarn || h.prox.Enabled(ctx, l)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		var b strings.Builder
		b.WriteString(r.Message)
		b.WriteString(h.attrs)
		r.Attrs(func(a slog.Attr) bool {
			escrever(&b, h.grupo, a)
			return true
		})
		h.anel.guardar(Linha{Em: r.Time, Nivel: r.Level.String(), Texto: b.String()})
	}
	if !h.prox.Enabled(ctx, r.Level) {
		return nil
	}
	return h.prox.Handle(ctx, r)
}

func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	var b strings.Builder
	b.WriteString(h.attrs)
	for _, a := range as {
		escrever(&b, h.grupo, a)
	}
	return &Handler{prox: h.prox.WithAttrs(as), anel: h.anel, attrs: b.String(), grupo: h.grupo}
}

func (h *Handler) WithGroup(nome string) slog.Handler {
	if nome == "" {
		return h
	}
	return &Handler{prox: h.prox.WithGroup(nome), anel: h.anel, attrs: h.attrs, grupo: h.grupo + nome + "."}
}

// Recentes devolve as linhas guardadas, da mais nova para a mais velha, e
// quantos avisos e erros houve desde que o dwnvr subiu - o total diz se o que
// aparece é tudo ou só a cauda.
func (h *Handler) Recentes() ([]Linha, int64) {
	a := h.anel
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.prox
	if a.cheio {
		n = len(a.linhas)
	}
	out := make([]Linha, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, a.linhas[(a.prox-i+len(a.linhas))%len(a.linhas)])
	}
	return out, a.total
}

// escrever acrescenta " chave=valor", com aspas quando o valor tem espaço -
// o mesmo critério do TextHandler, para a linha ler igual à do docker logs.
func escrever(b *strings.Builder, prefixo string, a slog.Attr) {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		p := prefixo
		if a.Key != "" {
			p += a.Key + "."
		}
		for _, g := range v.Group() {
			escrever(b, p, g)
		}
		return
	}
	if a.Key == "" {
		return
	}
	s := v.String()
	if s == "" || strings.ContainsAny(s, " \"=\n\t") {
		s = strconv.Quote(s)
	}
	b.WriteByte(' ')
	b.WriteString(prefixo)
	b.WriteString(a.Key)
	b.WriteByte('=')
	b.WriteString(s)
}
