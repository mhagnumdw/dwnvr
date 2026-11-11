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

// handleSaveCamera registers or changes a camera.
//
// The order matters: first cameras.json is written (atomically), only then the
// manager is notified. That way a crash between the two leaves the
// configuration saved and the recorder starts on the next boot - while the
// reverse order would make the recording start and vanish without a trace.
func (s *Server) handleSaveCamera(w http.ResponseWriter, r *http.Request) {
	var cam config.Camera
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&cam); err != nil {
		writeError(w, http.StatusBadRequest, "corpo inválido")
		return
	}

	cam = cam.WithAreaIDs()
	if err := validateCamera(s.cfg, cam); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Only streams that go2rtc actually serves can be registered. Without this,
	// a typo would become a camera that never records and whose cause shows up
	// nowhere.
	if streams, err := s.client.Streams(r.Context()); err == nil {
		if _, ok := streams[cam.ID]; !ok {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("o go2rtc não tem nenhum stream chamado %q", cam.ID))
			return
		}
	}

	// Only from here down: the go2rtc query above can take up to 10 s, and
	// holding the registry during it would make another camera's save wait for
	// nothing.
	s.registry.Lock()
	defer s.registry.Unlock()

	// What the camera didn't define and arrives equal to the default doesn't go
	// to cameras.json: the screen sends the resolved camera, and writing it
	// that way would pin it to today's defaults (see DropNewDefaults). It stays
	// in here because it compares with the registry, which only stays still
	// under the lock.
	cams := s.mgr.Cameras()
	found := false
	for i := range cams {
		if cams[i].ID == cam.ID {
			cam = s.cfg.DropNewDefaults(cam, &cams[i])
			cams[i] = cam
			found = true
			break
		}
	}
	if !found {
		cam = s.cfg.DropNewDefaults(cam, nil)
		cams = append(cams, cam)

		// A new camera may not be new on disk: one removed without deleting the
		// recordings and registered again with the same ID. Its index is read
		// here, before Set starts the recorder - see store.Load.
		start := time.Now()
		idx, err := s.store.Load(cam.ID)
		if err != nil {
			s.fail(w, "lendo o índice da câmera", err)
			return
		}
		if dias := idx.Days(); len(dias) > 0 {
			s.log.Info("index loaded", "cam", cam.ID, "days", len(dias),
				"mb", idx.TotalBytes()>>20, "took", time.Since(start).Round(time.Millisecond))
		}
	}

	if err := s.cfg.SaveCameras(cams); err != nil {
		s.fail(w, "gravando cameras.json", err)
		return
	}
	s.mgr.Set(cam)

	resolved := s.cfg.Resolve(cam)
	s.log.Info("camera saved", "cam", cam.ID, "enabled", cam.Enabled, "audio", resolved.Audio,
		"detect", *resolved.Detect, "detect_method", resolved.DetectMethod,
		"detect_sensitivity", resolved.DetectSensitivity)
	writeJSON(w, map[string]any{"ok": true, "camera": resolved})
}

// handleDeleteCamera takes the camera out of dwnvr.
//
// Recordings already made are only deleted with `recordings=1` in the URL. The
// default is still to keep them: deleting hours of video as a side effect of a
// click on "remove" would be too destructive to be implicit. Without the
// parameter, the material stays on disk and starts showing up in the orphan
// listing.
func (s *Server) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")

	// Until the end, Purge included: a save of the same camera right after
	// Remove would start a recorder writing into the directory being deleted.
	s.registry.Lock()
	defer s.registry.Unlock()

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
		s.log.Info("camera removed", "cam", id, "recordings_kept_at", dir)
		writeJSON(w, map[string]any{"ok": true, "recordingsKeptAt": dir})
		return
	}

	// Only now, with the Remove above having already waited for the open segment
	// to be closed and indexed. Purging before stopping the recorder would make
	// the EnsureDirs of the next segment recreate the just-deleted directory.
	freed, err := s.store.Camera(id).Purge()
	if err != nil {
		s.fail(w, "apagando as gravações", err)
		return
	}
	s.store.Forget(id)

	s.log.Info("camera removed with its recordings", "cam", id, "freed_mb", freed>>20)
	writeJSON(w, map[string]any{"ok": true, "recordingsDeleted": true, "freedBytes": freed})
}

// validateCamera rejects the camera with any field out of range. The validation
// is the same as at boot (config.CheckCamera), but here the outcome is
// different: in front of the screen there is someone to read the message and
// fix it on the spot, so instead of swapping in the default in silence, the API
// returns what is wrong.
func validateCamera(cfg *config.Config, cam config.Camera) error {
	if err := config.ValidateCameraID(cam.ID); err != nil {
		return err
	}
	_, warnings := cfg.CheckCamera(cam)
	if len(warnings) == 0 {
		return nil
	}
	texts := make([]string, len(warnings))
	for i, a := range warnings {
		texts[i] = a.Text()
	}
	return errors.New(strings.Join(texts, "; "))
}
