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

// running é um recorder no ar mais o que é preciso para pará-lo sozinho.
type running struct {
	rec    *Recorder
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager mantém um recorder por câmera habilitada e permite cadastrar,
// alterar e remover câmeras sem reiniciar o processo - cadastrar uma câmera e
// ter de reiniciar o serviço para ela começar a gravar seria um jeito ruim de
// perder gravação.
type Manager struct {
	cfg    *config.Config
	client *go2rtc.Client
	store  *store.Store
	log    *slog.Logger

	mu   sync.RWMutex
	recs map[string]*running
	cams map[string]config.Camera

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewManager(cfg *config.Config, client *go2rtc.Client, st *store.Store, log *slog.Logger) *Manager {
	return &Manager{
		cfg: cfg, client: client, store: st, log: log,
		recs: map[string]*running{}, cams: map[string]config.Camera{},
	}
}

// Start sobe os recorders das câmeras informadas.
func (m *Manager) Start(ctx context.Context, cams []config.Camera) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	for _, cam := range cams {
		m.Set(cam)
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sampleLoop(m.ctx)
	}()
}

// Set cadastra ou atualiza uma câmera, subindo, derrubando ou reiniciando o
// recorder conforme a configuração pedir.
func (m *Manager) Set(raw config.Camera) {
	cam := m.cfg.Resolve(raw)

	m.mu.Lock()
	m.cams[cam.ID] = raw
	old := m.recs[cam.ID]
	m.mu.Unlock()

	// Uma câmera desabilitada não deve ficar com recorder no ar.
	if !cam.Enabled {
		if old != nil {
			m.stop(cam.ID)
			m.log.Info("câmera desabilitada, gravação encerrada", "cam", cam.ID)
		}
		return
	}

	// Reiniciar só quando algo que o recorder usa de fato mudou: trocar o nome
	// de exibição não deveria abrir um buraco na gravação.
	if old != nil {
		if !recordingParamsChanged(old.rec.cam, cam) {
			m.mu.Lock()
			old.rec.cam = cam
			m.mu.Unlock()
			return
		}
		m.stop(cam.ID)
		m.log.Info("configuração de gravação mudou, reiniciando", "cam", cam.ID)
	}

	m.start(cam)
}

// recordingParamsChanged diz se a mudança exige reabrir a conexão. Nome e cota
// não exigem: a cota é aplicada pela retenção, que lê a configuração a cada
// passada.
func recordingParamsChanged(a, b config.Camera) bool {
	return a.Audio != b.Audio || a.SegmentSeconds != b.SegmentSeconds ||
		a.StallSeconds != b.StallSeconds
}

func (m *Manager) start(cam config.Camera) {
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	rec := newRecorder(cam, m.client, m.store.Camera(cam.ID), m.log)
	r := &running{rec: rec, cancel: cancel, done: make(chan struct{})}

	m.mu.Lock()
	m.recs[cam.ID] = r
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(r.done)
		rec.run(ctx)
	}()
}

// stop encerra o recorder de uma câmera e espera o segmento em aberto ser
// fechado e indexado.
func (m *Manager) stop(id string) {
	m.mu.Lock()
	r := m.recs[id]
	delete(m.recs, id)
	m.mu.Unlock()

	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// Pause para o recorder da câmera, executa fn e o sobe de volta.
//
// É o que permite apagar as gravações de uma câmera que está gravando. Um
// RemoveAll com o segmento aberto deixaria o writer despejando bytes num inode
// já desvinculado, e o finish() gravaria no índice uma entrada apontando para
// arquivo que não existe mais - estrago que só a reconciliação do próximo boot
// desfaria. O stop() daqui espera o segmento em aberto ser fechado e indexado
// antes de fn começar.
//
// Ao contrário de Remove, a câmera continua cadastrada o tempo todo: quem
// consultar /api/health no meio da operação vê uma câmera parada, não uma
// câmera que sumiu. ID sem cadastro - o caso de uma câmera já removida - só
// executa fn, porque não há recorder algum para parar.
func (m *Manager) Pause(id string, fn func() error) error {
	m.mu.RLock()
	raw, cadastrada := m.cams[id]
	m.mu.RUnlock()

	if !cadastrada {
		return fn()
	}

	m.stop(id)
	// O recorder volta mesmo se fn falhar: deixar a câmera parada por causa de
	// um erro em outra coisa transformaria uma operação falha em perda de
	// gravação até alguém perceber.
	cam := m.cfg.Resolve(raw)
	if cam.Enabled {
		defer m.start(cam)
	}
	return fn()
}

// Remove tira a câmera do gerenciador. As gravações no disco não são tocadas
// aqui: quem quiser apagá-las junto pede isso explicitamente na API, que chama
// o Purge depois deste stop. Apagar horas de vídeo como efeito colateral de um
// clique em "remover" seria destrutivo demais para ser implícito.
func (m *Manager) Remove(id string) {
	m.stop(id)
	m.mu.Lock()
	delete(m.cams, id)
	m.mu.Unlock()
	m.log.Info("câmera removida do gerenciador", "cam", id)
}

// Cameras devolve a configuração corrente, e é a fonte usada pela retenção e
// pela API para que todos enxerguem a mesma lista.
func (m *Manager) Cameras() []config.Camera {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]config.Camera, 0, len(m.cams))
	for _, c := range m.cams {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) sampleLoop(ctx context.Context) {
	t := time.NewTicker(bitrateSampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, r := range m.recorders() {
				r.sampleBitrate(now)
				r.checkSilence(now)
			}
		}
	}
}

func (m *Manager) recorders() []*Recorder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Recorder, 0, len(m.recs))
	for _, r := range m.recs {
		out = append(out, r.rec)
	}
	return out
}

// Status devolve a saúde de todas as câmeras cadastradas, inclusive as
// desabilitadas - sumir da tela de diagnóstico é a pior forma de sinalizar que
// uma câmera parou.
func (m *Manager) Status() []Status {
	m.mu.RLock()
	cams := make([]config.Camera, 0, len(m.cams))
	for _, c := range m.cams {
		cams = append(cams, m.cfg.Resolve(c))
	}
	recs := make(map[string]*Recorder, len(m.recs))
	for id, r := range m.recs {
		recs[id] = r.rec
	}
	m.mu.RUnlock()

	out := make([]Status, 0, len(cams))
	for _, cam := range cams {
		if r, ok := recs[cam.ID]; ok {
			out = append(out, r.Status())
			continue
		}
		// Câmera sem recorder (desabilitada) continua com o que já gravou em
		// disco: o passado dela precisa aparecer na tela igual ao das outras,
		// senão desabilitar uma câmera dá a impressão de ter apagado tudo.
		idx := m.store.Camera(cam.ID)
		disk, oldest, newest := idx.Resumo()
		st := Status{
			ID: cam.ID, Name: cam.Name, Enabled: cam.Enabled,
			QuotaMB: cam.QuotaMB, DiskBytes: disk,
		}
		var span int64
		if oldest > 0 {
			st.OldestSegmentAt = time.UnixMilli(oldest)
			span = newest - oldest
		}
		// Sem recorder não há taxa instantânea, mas a densidade do que está em
		// disco não precisa dela: a estimativa da cota aparece igual à das
		// câmeras ligadas, em vez de virar "-" só por estar desabilitada.
		st.RetainDays = retainDays(cam.QuotaMB, disk, span, 0)
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Stop encerra tudo e espera cada segmento em aberto ser fechado e indexado.
// Sem isso, todo reinício perderia o último minuto de cada câmera.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}
