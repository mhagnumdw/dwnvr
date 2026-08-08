package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// handleSaveCamera cadastra ou altera uma câmera.
//
// A ordem importa: primeiro o cameras.json é gravado (de forma atômica), só
// depois o gerenciador é avisado. Assim uma queda entre as duas coisas deixa a
// configuração salva e o recorder sobe no próximo boot — enquanto a ordem
// inversa faria a gravação começar e sumir sem deixar rastro.
func (s *Server) handleSaveCamera(w http.ResponseWriter, r *http.Request) {
	var cam config.Camera
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&cam); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	if err := validateCamera(cam); err != nil {
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
	}

	if err := s.cfg.SaveCameras(cams); err != nil {
		s.fail(w, "gravando cameras.json", err)
		return
	}
	s.mgr.Set(cam)

	s.log.Info("câmera salva", "cam", cam.ID, "habilitada", cam.Enabled, "audio", cam.Audio)
	writeJSON(w, map[string]any{"ok": true, "camera": s.cfg.Resolve(cam)})
}

// handleDeleteCamera tira a câmera do dwnvr.
//
// As gravações já feitas NÃO são apagadas: apagar horas de vídeo como efeito
// colateral de um clique seria destrutivo demais para ser implícito. Quem
// quiser o espaço de volta apaga o diretório da câmera.
func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
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

	dir := s.store.Camera(id).Dir()
	s.log.Info("câmera removida", "cam", id, "gravacoes_mantidas_em", dir)
	writeJSON(w, map[string]any{"ok": true, "recordingsKeptAt": dir})
}

func validateCamera(cam config.Camera) error {
	if err := config.ValidateCameraID(cam.ID); err != nil {
		return err
	}
	if cam.Audio != "" {
		if err := config.ValidAudio(cam.Audio); err != nil {
			return err
		}
	}
	if cam.QuotaMB < 0 {
		return fmt.Errorf("cota não pode ser negativa")
	}
	if cam.SegmentSeconds < 0 || cam.SegmentSeconds > 3600 {
		return fmt.Errorf("duração de segmento fora do intervalo aceito")
	}
	if cam.MaxDays < 0 {
		return fmt.Errorf("idade máxima não pode ser negativa")
	}
	return nil
}
