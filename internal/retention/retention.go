// Package retention mantém o disco sob controle apagando as gravações mais
// antigas.
//
// São três limites, aplicados nesta ordem:
//
//  1. cota em MB por câmera - o principal, é o que o usuário configura
//  2. idade máxima por câmera - opcional, para quem pensa em dias e não em GB
//  3. disco livre mínimo, global - a rede de segurança
//
// O terceiro existe porque a soma das cotas erra fácil: com 9 câmeras cada uma
// tem uma taxa diferente, e basta o usuário superestimar para o disco encher.
// Encher o disco é pior que perder gravação antiga, então esse limite ignora as
// cotas individuais e evicta o dia mais antigo entre as câmeras cadastradas.
//
// Gravação de câmera removida (órfã) não entra em nenhum dos três, nem no
// terceiro, e é de propósito: quem remove a câmera sem marcar "apagar também
// as gravações" as preservou, e o dwnvr não apaga sozinho o que o usuário
// guardou - nem quando a alternativa é apagar gravação de câmera no ar. O que
// ele faz é não esconder: os avisos do disco dizem quanto há em órfãs e onde
// apagá-las, e a tela de Diagnóstico as mostra à parte.
package retention

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// Interval é o intervalo entre passadas. Um minuto é bem mais frequente que o
// necessário para a cota, mas mantém a reação rápida quando o disco aperta.
const Interval = time.Minute

type Manager struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger

	// cameras é consultado a cada passada, e não copiado na subida: câmeras
	// podem ser cadastradas, alteradas e removidas com o dwnvr no ar, e uma
	// cota nova que só valesse após reiniciar seria uma armadilha.
	cameras func() []config.Camera

	// agora e livre são o relógio e a leitura do disco livre. Só os testes os
	// trocam: com eles, dá para envelhecer um dia ou apertar o disco sem
	// esperar nem encher um disco de verdade.
	agora func() time.Time
	livre func(path string) (int64, error)

	// abaixoDesde é quando a passada viu o disco abaixo do mínimo pela
	// primeira vez; zero enquanto ele está acima. É o "desde" do aviso da tela
	// de diagnóstico, com a precisão de uma passada.
	mu          sync.Mutex
	abaixoDesde time.Time
}

func New(cfg *config.Config, st *store.Store, cameras func() []config.Camera, log *slog.Logger) *Manager {
	return &Manager{cfg: cfg, store: st, cameras: cameras, log: log,
		agora: time.Now, livre: FreeBytes}
}

// resolved devolve a configuração corrente com os padrões já aplicados.
func (m *Manager) resolved() []config.Camera {
	cams := m.cameras()
	out := make([]config.Camera, 0, len(cams))
	for _, c := range cams {
		out = append(out, m.cfg.Resolve(c))
	}
	return out
}

// Run aplica a retenção periodicamente até o contexto ser cancelado.
func (m *Manager) Run(ctx context.Context) {
	// Uma passada logo na subida: se o processo ficou parado com o disco
	// cheio, não faz sentido esperar o primeiro tique para reagir.
	if err := m.Enforce(); err != nil {
		m.log.Error("retenção falhou", "erro", err)
	}

	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Enforce(); err != nil {
				m.log.Error("retenção falhou", "erro", err)
			}
		}
	}
}

// Enforce roda uma passada completa dos três limites.
func (m *Manager) Enforce() error {
	for _, cam := range m.resolved() {
		if err := m.enforceQuota(cam); err != nil {
			return err
		}
		if err := m.enforceMaxDays(cam); err != nil {
			return err
		}
	}
	return m.enforceFreeSpace()
}

func (m *Manager) enforceQuota(cam config.Camera) error {
	idx := m.store.Camera(cam.ID)
	quota := cam.QuotaMB << 20
	total := idx.TotalBytes()
	if quota <= 0 || total <= quota {
		return nil
	}

	want := total - quota
	freed, err := idx.EvictOldest(want)
	if err != nil {
		return err
	}
	// Câmera cheia apaga o excedente de cada minuto, para sempre: é o regime
	// normal, e em Info seria uma linha por câmera por minuto. Em KB porque o
	// excedente de um minuto é um ou dois segmentos, e em MB daria zero.
	attrs := []any{"cam", cam.ID,
		"usado_mb", total >> 20, "cota_mb", cam.QuotaMB, "liberado_kb", freed >> 10}
	if freed < want {
		m.log.Warn("cota excedida, mas não havia gravação suficiente para apagar",
			append(attrs, "faltou_kb", (want-freed)>>10)...)
		return nil
	}
	m.log.Debug("cota excedida, evictando", attrs...)
	return nil
}

func (m *Manager) enforceMaxDays(cam config.Camera) error {
	if cam.MaxDays <= 0 {
		return nil
	}
	idx := m.store.Camera(cam.ID)
	cutoff := m.agora().AddDate(0, 0, -cam.MaxDays).Format(store.DayLayout)

	for _, d := range idx.Days() {
		if d.Day >= cutoff {
			break // Days() vem ordenado, então daqui em diante é tudo recente
		}
		freed, err := idx.DropDay(d.Day)
		if err != nil {
			return err
		}
		m.log.Info("dia além da idade máxima, removido",
			"cam", cam.ID, "dia", d.Day, "liberado_mb", freed>>20)
	}
	return nil
}

// AbaixoDoMinimoDesde diz desde quando o disco está abaixo do mínimo livre, ou
// zero se ele não está.
func (m *Manager) AbaixoDoMinimoDesde() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.abaixoDesde
}

// anotaDisco guarda a transição do disco: só a primeira passada abaixo do
// mínimo marca o instante, e a primeira acima o apaga.
func (m *Manager) anotaDisco(abaixo bool, agora time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case !abaixo:
		m.abaixoDesde = time.Time{}
	case m.abaixoDesde.IsZero():
		m.abaixoDesde = agora
	}
}

// enforceFreeSpace é a rede de segurança global: evicta o dia mais antigo de
// qualquer câmera cadastrada até o disco voltar ao mínimo aceitável.
func (m *Manager) enforceFreeSpace() error {
	minFree := m.cfg.Storage.MinFreeMB << 20
	if minFree <= 0 {
		m.anotaDisco(false, m.agora())
		return nil
	}

	// Lido uma vez, e só com o disco já abaixo do mínimo: com o disco folgado,
	// que é o normal, a passada não varre o storage atrás de órfãs.
	orfas := int64(-1)
	semCamera := func(attrs []any) []any {
		if orfas < 0 {
			orfas = m.orfasBytes()
		}
		if orfas == 0 {
			return attrs
		}
		return append(attrs, "orfas_mb", orfas>>20,
			"apagar_em", "Câmeras > Gravações de câmeras removidas")
	}

	for {
		free, err := m.livre(m.store.Root())
		if err != nil {
			return err
		}
		m.anotaDisco(free < minFree, m.agora())
		if free >= minFree {
			return nil
		}

		cam, day, ok := m.oldestDay()
		if !ok {
			attrs := semCamera([]any{"livre_mb", free >> 20, "minimo_mb", m.cfg.Storage.MinFreeMB})
			msg := "disco abaixo do mínimo livre, mas não há mais nada a evictar"
			if orfas > 0 {
				// A frase antiga seria falsa: há gravação ali, só que de câmera
				// removida, que a retenção não apaga.
				msg = "disco abaixo do mínimo livre, e só restam gravações de câmeras removidas, que a retenção não apaga"
			}
			m.log.Warn(msg, attrs...)
			return nil
		}

		freed, err := m.store.Camera(cam).DropDay(day)
		if err != nil {
			return err
		}
		m.log.Warn("disco abaixo do mínimo livre, evictando o dia mais antigo",
			semCamera([]any{"livre_mb", free >> 20, "minimo_mb", m.cfg.Storage.MinFreeMB,
				"cam", cam, "dia", day, "liberado_mb", freed >> 20})...)

		if freed == 0 {
			// Nada foi liberado: sem isto o laço giraria para sempre quando o
			// espaço estivesse sendo consumido por algo fora do dwnvr.
			return nil
		}
	}
}

// orfasBytes soma o que há em disco de câmeras removidas. Só serve ao aviso:
// uma falha na varredura vira zero, e o aviso sai sem o campo.
func (m *Manager) orfasBytes() int64 {
	registered := map[string]bool{}
	for _, c := range m.cameras() {
		registered[c.ID] = true
	}
	total, err := m.store.OrphanBytes(registered)
	if err != nil {
		m.log.Error("listando gravações órfãs", "erro", err)
		return 0
	}
	return total
}

// oldestDay encontra o dia mais antigo entre as câmeras cadastradas.
func (m *Manager) oldestDay() (cam, day string, ok bool) {
	for _, c := range m.resolved() {
		days := m.store.Camera(c.ID).Days()
		if len(days) == 0 {
			continue
		}
		if !ok || days[0].Day < day {
			cam, day, ok = c.ID, days[0].Day, true
		}
	}
	return cam, day, ok
}
