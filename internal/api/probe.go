package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
)

// probeTTL é quanto tempo uma sonda vale. Curto porque a resposta pode mudar sem
// aviso - basta o dono editar o go2rtc.yaml -, e longo o bastante para abrir e
// fechar o formulário algumas vezes sem reconectar na câmera a cada clique.
const probeTTL = 5 * time.Minute

// probeEntry é uma sonda guardada. A URL do produtor entra na comparação porque
// é ela que identifica a FONTE: trocar o RTSP de um stream mantendo o nome
// invalidaria o resultado sem mudar a chave do cache.
type probeEntry struct {
	probe     go2rtc.AudioProbe
	sourceURL string
	at        time.Time
}

// probeCache guarda o resultado das sondas e serializa as concorrentes.
//
// O mutex é um só, e não um por stream, de propósito: sondar é abrir conexão com
// câmera, e duas telas abertas ao mesmo tempo não devem virar várias conexões
// simultâneas num dispositivo com 1,5 GB de RAM. Cada sonda dura poucos segundos.
type probeCache struct {
	mu      sync.Mutex
	entries map[string]probeEntry
}

func (c *probeCache) get(name, sourceURL string) (go2rtc.AudioProbe, bool) {
	e, ok := c.entries[name]
	if !ok || e.sourceURL != sourceURL || time.Since(e.at) > probeTTL {
		return go2rtc.AudioProbe{}, false
	}
	return e.probe, true
}

func (c *probeCache) put(name, sourceURL string, p go2rtc.AudioProbe) {
	if c.entries == nil {
		c.entries = map[string]probeEntry{}
	}
	c.entries[name] = probeEntry{probe: p, sourceURL: sourceURL, at: time.Now()}
}

// handleProbeStream responde se um stream entrega áudio, sondando-o se preciso.
//
// A tela de cadastro precisa dessa resposta ANTES de a câmera existir, e é
// justamente aí que o go2rtc não sabe dá-la: ele só preenche `medias` enquanto
// alguém consome o stream. Ver go2rtc.Client.ProbeAudio para o mecanismo.
//
// Stream que já tem produtor vivo - toda câmera cadastrada e gravando - não é
// sondado: o `medias` dele já está preenchido, e abrir uma segunda conexão só
// para perguntar o que já se sabe custaria uma sessão a mais com a câmera.
func (s *Server) handleProbeStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("src")
	if name == "" {
		writeError(w, http.StatusBadRequest, "faltou o parâmetro src")
		return
	}

	streams, err := s.client.Streams(r.Context())
	if err != nil {
		s.log.Error("listando streams do go2rtc", "erro", err)
		writeError(w, http.StatusBadGateway, "o go2rtc não respondeu")
		return
	}
	st, ok := streams[name]
	if !ok {
		writeError(w, http.StatusNotFound, "o go2rtc não tem nenhum stream com esse nome")
		return
	}

	// probed diz se a resposta custou uma conexão nova com a câmera. É o que
	// permite à tela distinguir "acabei de verificar" de "isto já se sabia".
	resp := map[string]any{"name": name, "hasAudio": false, "probed": false}
	setAudio := func(has bool, codecs []string) {
		resp["hasAudio"] = has
		if len(codecs) > 0 {
			resp["audioCodecs"] = codecs
		}
	}

	// O que distingue stream vivo de stream ocioso é o `medias`, não a presença
	// do produtor: o go2rtc lista o produtor CONFIGURADO desde sempre, só com a
	// url, e só preenche as trilhas quando de fato conecta na câmera.
	var sourceURL string
	conhecido := false
	for _, p := range st.Producers {
		sourceURL = p.URL
		if len(p.Medias) == 0 {
			continue
		}
		conhecido = true
		if p.HasAudio() {
			setAudio(true, p.AudioCodecs())
		}
	}
	if conhecido {
		writeJSON(w, resp)
		return
	}

	s.probes.mu.Lock()
	defer s.probes.mu.Unlock()

	if p, ok := s.probes.get(name, sourceURL); ok {
		setAudio(p.HasAudio, p.Codecs)
		writeJSON(w, resp)
		return
	}

	probe, err := s.client.ProbeAudio(r.Context(), name)
	if err != nil {
		// Não é erro de servidor: a câmera pode simplesmente estar fora do ar, e
		// a tela sabe o que fazer com isso - deixar as opções de áudio
		// clicáveis, em vez de mentir que a câmera não tem microfone. Por isso o
		// 200 com o motivo junto, e nada guardado no cache.
		s.log.Warn("sondando áudio do stream", "stream", name, "erro", err)
		resp["erro"] = err.Error()
		writeJSON(w, resp)
		return
	}

	s.probes.put(name, sourceURL, probe)
	resp["probed"] = true
	setAudio(probe.HasAudio, probe.Codecs)
	writeJSON(w, resp)
}
