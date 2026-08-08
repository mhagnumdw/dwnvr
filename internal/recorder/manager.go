package recorder

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// bitrateSampleInterval é de quanto em quanto tempo a taxa observada é
// reamostrada. Precisa ser bem menor que a duração de um segmento para que o
// diagnóstico não fique estagnado entre um segmento e outro.
const bitrateSampleInterval = 15 * time.Second

// Manager sobe um recorder por câmera habilitada e mantém a amostragem de taxa.
type Manager struct {
	cfg    *config.Config
	client *go2rtc.Client
	store  *store.Store
	log    *slog.Logger

	mu   sync.RWMutex
	recs map[string]*Recorder

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewManager(cfg *config.Config, client *go2rtc.Client, st *store.Store, log *slog.Logger) *Manager {
	return &Manager{cfg: cfg, client: client, store: st, log: log, recs: map[string]*Recorder{}}
}

// Start sobe os recorders das câmeras habilitadas e com gravação pedida.
func (m *Manager) Start(ctx context.Context, cams []config.Camera) {
	ctx, m.cancel = context.WithCancel(ctx)

	for _, raw := range cams {
		cam := m.cfg.Resolve(raw)
		if !cam.Enabled {
			m.log.Info("câmera desabilitada, ignorando", "cam", cam.ID)
			continue
		}

		rec := newRecorder(cam, m.client, m.store.Camera(cam.ID), m.log)
		m.mu.Lock()
		m.recs[cam.ID] = rec
		m.mu.Unlock()

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			rec.run(ctx)
		}()
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sampleLoop(ctx)
	}()
}

func (m *Manager) sampleLoop(ctx context.Context) {
	t := time.NewTicker(bitrateSampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, r := range m.Recorders() {
				r.sampleBitrate(now)
			}
		}
	}
}

func (m *Manager) Recorders() []*Recorder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Recorder, 0, len(m.recs))
	for _, r := range m.recs {
		out = append(out, r)
	}
	return out
}

// Status devolve a saúde de todas as câmeras, ordenada por nome para que a tela
// de diagnóstico não fique dançando a cada atualização.
func (m *Manager) Status() []Status {
	recs := m.Recorders()
	out := make([]Status, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Status())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Stop encerra os recorders e espera cada segmento em aberto ser fechado e
// indexado — sem isso, todo reinício perderia o último minuto de cada câmera.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}
