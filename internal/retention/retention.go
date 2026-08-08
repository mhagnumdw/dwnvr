// Package retention mantém o disco sob controle apagando as gravações mais
// antigas.
//
// São três limites, aplicados nesta ordem:
//
//  1. cota em MB por câmera — o principal, é o que o usuário configura
//  2. idade máxima por câmera — opcional, para quem pensa em dias e não em GB
//  3. disco livre mínimo, global — a rede de segurança
//
// O terceiro existe porque a soma das cotas erra fácil: com 9 câmeras cada uma
// tem uma taxa diferente, e basta o usuário superestimar para o disco encher.
// Encher o disco é pior que perder gravação antiga, então esse limite ignora as
// cotas individuais e evicta o segmento mais antigo de todo o sistema.
package retention

import (
	"context"
	"log/slog"
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
}

func New(cfg *config.Config, st *store.Store, cameras func() []config.Camera, log *slog.Logger) *Manager {
	return &Manager{cfg: cfg, store: st, cameras: cameras, log: log}
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
	m.log.Info("cota excedida, evictando",
		"cam", cam.ID,
		"usado_mb", total>>20, "cota_mb", cam.QuotaMB, "liberado_mb", freed>>20)
	return nil
}

func (m *Manager) enforceMaxDays(cam config.Camera) error {
	if cam.MaxDays <= 0 {
		return nil
	}
	idx := m.store.Camera(cam.ID)
	cutoff := time.Now().AddDate(0, 0, -cam.MaxDays).Format(store.DayLayout)

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

// enforceFreeSpace é a rede de segurança global: evicta o dia mais antigo de
// qualquer câmera até o disco voltar ao mínimo aceitável.
func (m *Manager) enforceFreeSpace() error {
	minFree := m.cfg.Storage.MinFreeMB << 20
	if minFree <= 0 {
		return nil
	}

	for {
		free, err := FreeBytes(m.store.Root())
		if err != nil {
			return err
		}
		if free >= minFree {
			return nil
		}

		cam, day, ok := m.oldestDay()
		if !ok {
			m.log.Warn("disco abaixo do mínimo livre, mas não há mais nada a evictar",
				"livre_mb", free>>20, "minimo_mb", m.cfg.Storage.MinFreeMB)
			return nil
		}

		freed, err := m.store.Camera(cam).DropDay(day)
		if err != nil {
			return err
		}
		m.log.Warn("disco abaixo do mínimo livre, evictando o dia mais antigo",
			"livre_mb", free>>20, "minimo_mb", m.cfg.Storage.MinFreeMB,
			"cam", cam, "dia", day, "liberado_mb", freed>>20)

		if freed == 0 {
			// Nada foi liberado: sem isto o laço giraria para sempre quando o
			// espaço estivesse sendo consumido por algo fora do dwnvr.
			return nil
		}
	}
}

// oldestDay encontra o dia mais antigo entre todas as câmeras.
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
