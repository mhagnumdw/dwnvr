package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// gapTolerance is the slack for treating two segments as contiguous on the
// timeline. Consecutive segments meet a few milliseconds apart because of
// duration rounding; without this slack the 24h bar would be covered with fake
// 1-pixel gaps.
const gapTolerance = 2000 * time.Millisecond

// maxExportSpan caps the export window. Without a cap, a 30-day request would
// try to splice tens of thousands of segments into a single response. The value
// is the same cap as the Recordings screen (DURACOES in
// web/src/routes/Recordings.svelte), so that whoever calls the API directly
// can't ask for more than the interface offers.
const maxExportSpan = 10 * time.Minute

// handleDays lists the days with recordings - the dots on the calendar.
func (s *Server) handleDays(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"cam": cam.ID, "days": cam.Days()})
}

// handleDeleteRecordings deletes everything a camera recorded, without touching
// its registry entry.
//
// It serves both sides: the registered camera, which starts recording again
// from scratch right after, and the already removed camera, whose material can
// only be reached through here - the other recording endpoints go through
// knownCamera and don't see an ID without a registry entry.
func (s *Server) handleDeleteRecordings(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("cam")

	// Until the end, Purge included: a save of the same camera in the middle
	// would start a recorder writing into the directory being deleted, and
	// Pause, when it finished, would start another. This also holds for the
	// already removed camera, which a save can register again with the same ID.
	s.registry.Lock()
	defer s.registry.Unlock()

	if s.knownCamera(id) {
		// Pause is mandatory: purging with the recorder running would leave the
		// open segment writing to an already unlinked inode and would index a
		// file that no longer exists.
		var freed int64
		err := s.mgr.Pause(id, func() (err error) {
			freed, err = s.store.Camera(id).Purge()
			return err
		})
		if err != nil {
			s.fail(w, "apagando as gravações", err)
			return
		}
		s.log.Info("recordings deleted", "cam", id, "freed_mb", freed>>20)
		writeJSON(w, map[string]any{"ok": true, "freedBytes": freed})
		return
	}

	// Camera already removed. Here knownCamera can't serve as a guard, so the ID
	// is checked against the list of directories that actually exist: the names
	// come from a ReadDir and are always a single path component, which closes
	// directory traversal by the same principle - existence, not sanitization.
	orphans, err := s.store.Orphans(s.registeredIDs())
	if err != nil {
		s.fail(w, "listando gravações órfãs", err)
		return
	}
	for _, o := range orphans {
		if o.ID != id {
			continue
		}
		// The total comes from the scan, not from Purge: the in-memory index of
		// a camera without a registry entry is empty (nobody loaded it at
		// boot), so Purge would have no way to know the size of what it just
		// deleted.
		if _, err := s.store.Camera(id).Purge(); err != nil {
			s.fail(w, "apagando as gravações", err)
			return
		}
		s.store.Forget(id)
		s.log.Info("orphan recordings deleted", "cam", id, "freed_mb", o.Bytes>>20)
		writeJSON(w, map[string]any{"ok": true, "freedBytes": o.Bytes})
		return
	}

	writeError(w, http.StatusNotFound, "não há gravações para essa câmera")
}

// timelineResponse is deliberately compact: a day with 1-minute segments has
// 1440 entries, and the verbose form (an object with keys per segment) would
// triple the size of the response the timeline fetches on every day change.
type timelineResponse struct {
	Cam  string `json:"cam"`
	From int64  `json:"from"`
	To   int64  `json:"to"`
	// Ranges are contiguous spans [start, end] for drawing the bar.
	Ranges [][2]int64 `json:"ranges"`
	// Gens is the table of init generations; Segments refers to it by index.
	Gens []string `json:"gens"`
	// Segments is [startMs, durationMs, indexInTheGensTable].
	Segments [][3]int64 `json:"segments"`
}

// handleTimeline returns, in one go, what the bar needs to draw (ranges) and
// what the player needs to play (segments).
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}

	resp := timelineResponse{
		Cam: cam.ID, From: from, To: to,
		Ranges: [][2]int64{}, Gens: []string{}, Segments: [][3]int64{},
	}
	genIdx := map[string]int{}

	for _, e := range entries {
		i, ok := genIdx[e.Gen]
		if !ok {
			i = len(resp.Gens)
			genIdx[e.Gen] = i
			resp.Gens = append(resp.Gens, e.Gen)
		}
		resp.Segments = append(resp.Segments, [3]int64{e.StartMs, e.DurMs, int64(i)})

		// Extends the current range if the segment touches it; otherwise opens
		// another.
		n := len(resp.Ranges)
		if n > 0 && e.StartMs-resp.Ranges[n-1][1] <= gapTolerance.Milliseconds() {
			if end := e.EndMs(); end > resp.Ranges[n-1][1] {
				resp.Ranges[n-1][1] = end
			}
			continue
		}
		resp.Ranges = append(resp.Ranges, [2]int64{e.StartMs, e.EndMs()})
	}
	writeJSON(w, resp)
}

// eventsResponse is the list of onsets of a stretch, and it is deliberately
// compact for the same reason as timelineResponse above: a day at the most
// sensitive level has ~2,400 marks, and the verbose form (one object per mark)
// would double the response to say exactly the same thing.
type eventsResponse struct {
	Cam  string `json:"cam"`
	From int64  `json:"from"`
	To   int64  `json:"to"`
	// Onsets are the instants, in epoch ms, when the image changed enough to be
	// worth an inference. They come in ascending order.
	Onsets []int64 `json:"onsets"`

	// Objects are the marks the object detector confirmed: one per family per
	// inference, at the instant of the onset that triggered it. They are few -
	// dozens per camera per day -, so they go with full keys, unlike the
	// onsets. A camera without an object detector responds with an empty list.
	Objects []markedObject `json:"objetos"`
}

type markedObject struct {
	AtMs   int64   `json:"instanteMs"`
	Family string  `json:"familia"`
	Class  string  `json:"classe"`
	Score  float64 `json:"score"`
	// Where and when to draw the box over the video. A mark without them gets
	// no box on screen. See store.Event.
	FrameMs int64       `json:"quadroMs,omitempty"`
	Box     *[4]float64 `json:"caixa,omitempty"`
}

// handleEvents returns the marks of a stretch: the motion onsets, which the
// timeline heat strip draws, and the object detector's object marks.
//
// The TAIL PATH is what makes this route fit on an Orange Pi: on the current
// day the screen asks again every ten seconds, and asking for the whole day
// each time would resend thousands of marks to add two. Since `from` and `to`
// are free, the screen asks only for the part it doesn't have yet, the same way
// it already does with the timeline. A past day doesn't change and isn't asked
// for again.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	events, err := cam.EventRange(from, to)
	if err != nil {
		s.fail(w, "lendo as marcas de movimento", err)
		return
	}

	resp := eventsResponse{Cam: cam.ID, From: from, To: to, Onsets: []int64{}, Objects: []markedObject{}}
	for _, ev := range events {
		// The object mark lives in the same file, and can't go into the heat
		// strip: each object would count as a second onset at the same instant.
		if ev.IsObject() {
			resp.Objects = append(resp.Objects, markedObject{ev.AtMs, ev.Family, ev.Class, ev.Score, ev.FrameMs, ev.Box})
			continue
		}
		resp.Onsets = append(resp.Onsets, ev.AtMs)
	}
	writeJSON(w, resp)
}

// handleInit serves the init segment of a generation.
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	gen := r.URL.Query().Get("g")
	if err := validGen(gen); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The name is the hash of the content, so the file never changes: it can be
	// cached forever.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, cam.InitPath(gen))
}

// handleSegment serves the fragments of a segment, SKIPPING the init.
//
// The init is served separately and only once (via EXT-X-MAP or by the player),
// so resending it with each segment would be waste - and, in MSE, reattaching
// the init on every append is unnecessary.
func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request) {
	cam, entry, err := s.segmentParam(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	f, err := os.Open(cam.SegmentPath(entry.StartMs))
	if err != nil {
		writeError(w, http.StatusNotFound, "segmento não encontrado")
		return
	}
	defer f.Close()

	body := io.NewSectionReader(f, entry.InitSize, entry.Size-entry.InitSize)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	// ServeContent handles Range and If-None-Match on its own.
	http.ServeContent(w, r, "", time.UnixMilli(entry.StartMs), body)
}

// handleThumb returns init + first fragment: a single-frame MP4.
//
// It is the timeline thumbnail, and the whole point is that the server decodes
// nothing - it only cuts bytes that are already on disk. The browser does the
// decoding.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	cam, entry, err := s.segmentParam(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if entry.FirstFrag <= 0 {
		writeError(w, http.StatusNotFound, "segmento sem fragmento inicial indexado")
		return
	}

	f, err := os.Open(cam.SegmentPath(entry.StartMs))
	if err != nil {
		writeError(w, http.StatusNotFound, "segmento não encontrado")
		return
	}
	defer f.Close()

	body := io.NewSectionReader(f, 0, entry.InitSize+entry.FirstFrag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "", time.UnixMilli(entry.StartMs), body)
}

// handlePlaylist generates an HLS VOD playlist for the requested interval.
//
// The interface player uses MSE directly, but the playlist gives immediate
// compatibility with VLC, ffplay and Safari - which also makes it the most
// practical tool for debugging a recording without opening the browser.
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound, "nenhuma gravação no intervalo")
		return
	}

	var maxDur int64
	for _, e := range entries {
		if e.DurMs > maxDur {
			maxDur = e.DurMs
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", (maxDur+999)/1000)

	var (
		prevEnd int64
		prevGen string
	)
	for i, e := range entries {
		// Discontinuity at a recording gap and at an init change: in both cases
		// the decoder has to be reset.
		gap := i > 0 && e.StartMs-prevEnd > gapTolerance.Milliseconds()
		if i > 0 && (gap || e.Gen != prevGen) {
			fmt.Fprint(w, "#EXT-X-DISCONTINUITY\n")
		}
		if i == 0 || e.Gen != prevGen {
			fmt.Fprintf(w, "#EXT-X-MAP:URI=\"init?cam=%s&g=%s\"\n", cam.ID, e.Gen)
		}
		fmt.Fprintf(w, "#EXT-X-PROGRAM-DATE-TIME:%s\n",
			time.UnixMilli(e.StartMs).Format(time.RFC3339Nano))
		fmt.Fprintf(w, "#EXTINF:%.3f,\n", float64(e.DurMs)/1000)
		fmt.Fprintf(w, "seg?cam=%s&t=%d\n", cam.ID, e.StartMs)

		prevEnd, prevGen = e.EndMs(), e.Gen
	}
	fmt.Fprint(w, "#EXT-X-ENDLIST\n")
}

// handleExport splices the segments of an interval into a single MP4.
//
// It costs almost nothing: the media bytes are copied as they are and only the
// tfdt of each fragment is shifted to form a continuous timeline. Nothing is
// decoded or re-encoded, and the response is written as a stream, without
// loading the whole file into memory.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if time.Duration(to-from)*time.Millisecond > maxExportSpan {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("intervalo maior que o máximo de %s", maxExportSpan))
		return
	}

	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound, "nenhuma gravação no intervalo")
		return
	}

	// An MP4 can only have one init. Crossing a generation change would require
	// re-encoding or delivering two files; rejecting is more honest than
	// delivering something that only plays halfway.
	gen := entries[0].Gen
	for _, e := range entries {
		if e.Gen != gen {
			writeError(w, http.StatusConflict,
				"o intervalo atravessa uma troca de codec; exporte os trechos separadamente")
			return
		}
	}

	initBytes, err := os.ReadFile(cam.InitPath(gen))
	if err != nil {
		s.fail(w, "lendo o init segment", err)
		return
	}
	movie, err := fmp4.ParseMoov(initBytes[findMoov(initBytes):])
	if err != nil {
		s.fail(w, "init segment ilegível", err)
		return
	}
	video, ok := movie.VideoTrack()
	if !ok {
		s.fail(w, "init sem trilha de vídeo", errors.New("no video track"))
		return
	}

	name := fmt.Sprintf("%s_%s.mp4", cam.ID, time.UnixMilli(from).Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if _, err := w.Write(initBytes); err != nil {
		return
	}

	// offsetMs accumulates the duration already written: each segment starts at
	// zero, and this running total is what splices them into a continuous
	// timeline.
	var offsetMs int64
	for _, e := range entries {
		deltas := make(map[uint32]int64, len(movie.Tracks))
		for _, t := range movie.Tracks {
			deltas[t.ID] = offsetMs * int64(t.Timescale) / 1000
		}
		if err := s.appendSegment(w, cam, e, video.ID, deltas); err != nil {
			// The response has already started; the status can't be changed
			// now.
			s.log.Warn("export interrupted", "cam", cam.ID, "t", e.StartMs, "err", err)
			return
		}
		offsetMs += e.DurMs
	}
}

// appendSegment copies the fragments of a segment to w, shifting the tfdt.
func (s *Server) appendSegment(w io.Writer, cam *store.Camera, e store.Entry,
	videoTrackID uint32, deltas map[uint32]int64) error {

	f, err := os.Open(cam.SegmentPath(e.StartMs))
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(e.InitSize, io.SeekStart); err != nil {
		return err
	}

	rd := fmp4.NewReader(f)
	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if typ == "moof" {
			if err := fmp4.ShiftMoof(box, deltas); err != nil {
				return err
			}
		}
		if _, err := w.Write(box); err != nil {
			return err
		}
	}
}

// findMoov finds the moov box inside the init (which starts with ftyp).
func findMoov(init []byte) int {
	for off := 0; off+8 <= len(init); {
		size := int(be32u(init[off:]))
		if size < 8 || off+size > len(init) {
			break
		}
		if string(init[off+4:off+8]) == "moov" {
			return off
		}
		off += size
	}
	return 0
}

func be32u(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// --- parameters ------------------------------------------------------------

func (s *Server) camParam(r *http.Request) (*store.Camera, error) {
	id := r.URL.Query().Get("cam")
	if !s.knownCamera(id) {
		return nil, fmt.Errorf("câmera %q não cadastrada", id)
	}
	return s.store.Camera(id), nil
}

// segmentParam resolves cam+t to the matching index entry.
//
// The entry is looked up in the index instead of building the path directly
// from t: the index is what knows InitSize and FirstFrag, and going through it
// guarantees that only segments dwnvr actually recorded are served.
func (s *Server) segmentParam(r *http.Request) (*store.Camera, store.Entry, error) {
	cam, err := s.camParam(r)
	if err != nil {
		return nil, store.Entry{}, err
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil {
		return nil, store.Entry{}, errors.New("parâmetro t inválido")
	}

	e, ok, err := cam.Entry(t)
	if err != nil {
		return nil, store.Entry{}, err
	}
	if !ok {
		return nil, store.Entry{}, errors.New("segmento não encontrado no índice")
	}
	return cam, e, nil
}

func (s *Server) rangeParams(r *http.Request) (from, to int64, err error) {
	q := r.URL.Query()

	// Shortcut: day=2026-08-08 is the same as the whole day in local time.
	if day := q.Get("day"); day != "" {
		d, perr := time.ParseInLocation(store.DayLayout, day, time.Local)
		if perr != nil {
			return 0, 0, errors.New("parâmetro day inválido (use AAAA-MM-DD)")
		}
		return d.UnixMilli(), d.AddDate(0, 0, 1).UnixMilli(), nil
	}

	if from, err = strconv.ParseInt(q.Get("from"), 10, 64); err != nil {
		return 0, 0, errors.New("parâmetro from inválido")
	}
	if to, err = strconv.ParseInt(q.Get("to"), 10, 64); err != nil {
		return 0, 0, errors.New("parâmetro to inválido")
	}
	if to <= from {
		return 0, 0, errors.New("to precisa ser maior que from")
	}
	return from, to, nil
}

// genPattern is the truncated hash that names the init: lowercase hexadecimal,
// up to 32 characters.
var genPattern = regexp.MustCompile(`^[0-9a-f]{1,32}$`)

// validGen rejects anything that isn't the expected hexadecimal hash: the
// generation becomes a file name, and accepting a path here would open
// directory traversal.
//
// The check is a regexp, and not a loop over the characters, because that is
// the form CodeQL recognizes as a sanitizer: with the loop, go/path-injection
// flagged the ServeFile in handleInit even with g already validated. The regexp
// package already comes into the binary through yaml, so it costs nothing
// extra.
func validGen(gen string) error {
	if gen == "" {
		return errors.New("parâmetro g obrigatório")
	}
	if !genPattern.MatchString(gen) {
		return errors.New("parâmetro g inválido")
	}
	return nil
}
