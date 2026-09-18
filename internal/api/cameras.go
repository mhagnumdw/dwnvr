package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// minQuotaMB é o mesmo piso que o formulário da web cobra no campo de cota.
const minQuotaMB = 100

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

func validateCamera(cam config.Camera) error {
	if err := config.ValidateCameraID(cam.ID); err != nil {
		return err
	}
	if cam.Audio != "" {
		if err := config.ValidAudio(cam.Audio); err != nil {
			return err
		}
	}
	// Zero é "usar o default". Acima disso vale o mesmo piso que a tela cobra:
	// uma cota de poucos MB não guarda nem um segmento, e a câmera passaria a
	// vida apagando o que acabou de gravar.
	if cam.QuotaMB < 0 {
		return fmt.Errorf("cota não pode ser negativa")
	}
	if cam.QuotaMB > 0 && cam.QuotaMB < minQuotaMB {
		return fmt.Errorf("cota mínima é de %d MB", minQuotaMB)
	}
	if cam.SegmentSeconds < 0 || cam.SegmentSeconds > 3600 {
		return fmt.Errorf("duração de segmento fora do intervalo aceito")
	}
	if cam.MaxDays < 0 {
		return fmt.Errorf("idade máxima não pode ser negativa")
	}
	// Zero é "usar o default"; negativo ou absurdo desligaria na prática a
	// vigilância que impede a câmera de parar de gravar em silêncio.
	if cam.StallSeconds < 0 || cam.StallSeconds > 3600 {
		return fmt.Errorf("limiar de inatividade fora do intervalo aceito")
	}
	// Vazio e zero são "usar o default", e o Resolve cuida deles. O que veio
	// preenchido tem que ser um mecanismo que existe e um nível que existe.
	if cam.DetectMecanismo != "" || cam.DetectSensibilidade != 0 {
		mec, nivel := cam.DetectMecanismo, cam.DetectSensibilidade
		if mec == "" {
			mec = detect.MecanismoPadrao
		}
		if nivel == 0 {
			nivel = detect.NivelPadrao
		}
		if err := config.ValidDetect(mec, nivel); err != nil {
			return err
		}
	}
	return nil
}
