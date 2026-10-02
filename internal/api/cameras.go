package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// handleSaveCamera cadastra ou altera uma câmera.
//
// A ordem importa: primeiro o cameras.json é gravado (de forma atômica), só
// depois o gerenciador é avisado. Assim uma queda entre as duas coisas deixa a
// configuração salva e o recorder sobe no próximo boot - enquanto a ordem
// inversa faria a gravação começar e sumir sem deixar rastro.
func (s *Server) handleSaveCamera(w http.ResponseWriter, r *http.Request) {
	var cam config.Camera
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&cam); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if err := validateCamera(s.cfg, cam); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Só streams que o go2rtc realmente serve podem ser cadastrados. Sem isso,
	// um erro de digitação viraria uma câmera que nunca grava e cuja causa não
	// aparece em lugar nenhum.
	if streams, err := s.client.Streams(r.Context()); err == nil {
		if _, ok := streams[cam.ID]; !ok {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("o go2rtc não tem nenhum stream chamado %q", cam.ID))
			return
		}
	}

	// Só daqui para baixo: a consulta ao go2rtc acima pode levar até 10 s, e
	// segurar o cadastro nela faria o save de outra câmera esperar à toa.
	s.cadastro.Lock()
	defer s.cadastro.Unlock()

	cams := s.mgr.Cameras()
	found := false
	for i := range cams {
		if cams[i].ID == cam.ID {
			cams[i] = cam
			found = true
			break
		}
	}
	if !found {
		cams = append(cams, cam)

		// Câmera nova pode não ser nova no disco: uma removida sem apagar as
		// gravações e cadastrada de novo com o mesmo ID. O índice dela é lido
		// aqui, antes do Set subir o recorder - ver store.Load.
		inicio := time.Now()
		idx, err := s.store.Load(cam.ID)
		if err != nil {
			s.fail(w, "lendo o índice da câmera", err)
			return
		}
		if dias := idx.Days(); len(dias) > 0 {
			s.log.Info("índice carregado", "cam", cam.ID, "dias", len(dias),
				"mb", idx.TotalBytes()>>20, "em", time.Since(inicio).Round(time.Millisecond))
		}
	}

	if err := s.cfg.SaveCameras(cams); err != nil {
		s.fail(w, "gravando cameras.json", err)
		return
	}
	s.mgr.Set(cam)

	resolvida := s.cfg.Resolve(cam)
	s.log.Info("câmera salva", "cam", cam.ID, "habilitada", cam.Enabled, "audio", cam.Audio,
		"detect", *resolvida.Detect, "detect_mecanismo", resolvida.DetectMecanismo,
		"detect_sensibilidade", resolvida.DetectSensibilidade)
	writeJSON(w, map[string]any{"ok": true, "camera": resolvida})
}

// handleDeleteCamera tira a câmera do dwnvr.
//
// As gravações já feitas só são apagadas com `recordings=1` na URL. O padrão
// continua sendo preservá-las: apagar horas de vídeo como efeito colateral de um
// clique em "remover" seria destrutivo demais para ser implícito. Sem o
// parâmetro, o material fica em disco e passa a aparecer na listagem de órfãos.
func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	// Até o fim, Purge incluído: um save da mesma câmera logo depois do Remove
	// subiria um recorder gravando no diretório que está sendo apagado.
	s.cadastro.Lock()
	defer s.cadastro.Unlock()

	if !s.knownCamera(id) {
		writeError(w, http.StatusNotFound, "câmera não cadastrada")
		return
	}

	cams := s.mgr.Cameras()
	kept := make([]config.Camera, 0, len(cams))
	for _, c := range cams {
		if c.ID != id {
			kept = append(kept, c)
		}
	}

	if err := s.cfg.SaveCameras(kept); err != nil {
		s.fail(w, "gravando cameras.json", err)
		return
	}
	s.mgr.Remove(id)

	if r.URL.Query().Get("recordings") != "1" {
		dir := s.store.Camera(id).Dir()
		s.log.Info("câmera removida", "cam", id, "gravacoes_mantidas_em", dir)
		writeJSON(w, map[string]any{"ok": true, "recordingsKeptAt": dir})
		return
	}

	// Só agora, com o Remove acima já tendo esperado o segmento em aberto ser
	// fechado e indexado. Purgar antes de parar o recorder faria o EnsureDirs do
	// segmento seguinte recriar o diretório recém-apagado.
	freed, err := s.store.Camera(id).Purge()
	if err != nil {
		s.fail(w, "apagando as gravações", err)
		return
	}
	s.store.Forget(id)

	s.log.Info("câmera removida com as gravações", "cam", id, "liberado_mb", freed>>20)
	writeJSON(w, map[string]any{"ok": true, "recordingsDeleted": true, "freedBytes": freed})
}

// validateCamera recusa a câmera com qualquer campo fora da faixa. A régua é a
// mesma do boot (config.ConfereCamera), mas aqui o desfecho é outro: na frente
// da tela há alguém para ler a mensagem e corrigir na hora, então em vez de
// trocar pelo padrão em silêncio, a API devolve o que está errado.
func validateCamera(cfg *config.Config, cam config.Camera) error {
	if err := config.ValidateCameraID(cam.ID); err != nil {
		return err
	}
	_, avisos := cfg.ConfereCamera(cam)
	if len(avisos) == 0 {
		return nil
	}
	textos := make([]string, len(avisos))
	for i, a := range avisos {
		textos[i] = a.Texto()
	}
	return errors.New(strings.Join(textos, "; "))
}
