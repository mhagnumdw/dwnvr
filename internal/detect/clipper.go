package detect

// Clip is what goes to the object detector: a stretch of video that decodes on
// its own, from the I frame up to the frame to infer, which is its LAST one.
type Clip struct {
	Camera string

	// OnsetMs is where the mark sits on the timeline.
	OnsetMs int64

	// FrameMs is the instant of the frame to infer, the clip's last one. It may
	// be the onset itself, or the peak that came up to PeakWindowMs after it.
	FrameMs int64

	// Fmp4 is the connection's init followed by the fragments (moof+mdat) from
	// the I frame up to the frame to infer, inclusive. Any decoder opens it as
	// an .mp4 file.
	Fmp4 []byte
}

// Clipper keeps a camera's current GOP, so the clip sent to the object
// detector can be cut at the frame the Method picked - which may come up to
// PeakWindowMs after the onset, and not only at the onset.
//
// There are TWO buffers, not one, because the candidate may be left behind: if
// an I frame arrives with the window open, the GOP holding the candidate goes
// to the spare, and the new GOP starts in the other buffer. The candidate is
// always in one of the two, with any number of I frames inside the window.
//
// The buffers are reused: once they grow to the camera's largest GOP, storing
// a frame is just copying the box, with no allocation. Only the cut allocates -
// once per onset -, and it's the copy that goes on to the queue.
//
// Not safe for concurrent use: it belongs to the camera's recording goroutine,
// like the Method.
type Clipper struct {
	camera string
	init   []byte

	current, spare []byte

	// storing says whether an arriving frame goes into the current GOP: only
	// after the connection's first I frame, and only up to MaxGOPBytes. A
	// frame without its I frame doesn't decode.
	storing bool

	pending     bool
	onsetMs     int64
	candidateMs int64
	candInSpare bool
	// candidateEnd is how far the clip goes, in the candidate's buffer. Zero
	// means "no candidate stored", and no cut comes out.
	candidateEnd int

	// noVideo counts the onsets that ended without a clip. See NoVideo.
	noVideo int64
}

// NoVideo says how many onsets, since the clipper was created, ended without a
// clip to send to the object detector: the window closed with no frame stored
// - right after connecting, before the first I frame, or with the GOP above
// the cap -, or the onset was forgotten in the middle of the window.
//
// It's counted here, where it happens, and not derived from "onsets minus
// clips": the difference also includes an onset still waiting for its peak,
// which would show up as lost during the seconds of the window.
func (r *Clipper) NoVideo() int64 { return r.noVideo }

// drop ends the pending onset without a clip.
func (r *Clipper) drop() {
	if r.pending {
		r.noVideo++
	}
	r.pending, r.candidateEnd = false, 0
}

// NewClipper returns a camera's clipper. It only starts storing anything
// after the first Init.
func NewClipper(camera string) *Clipper {
	return &Clipper{camera: camera}
}

// Init starts a new connection. init is its ftyp+moov, which opens every
// clip; the GOP that was there doesn't continue in it and is forgotten. The
// caller must already have collected the pending clip with Close.
func (r *Clipper) Init(init []byte) {
	r.init = init
	r.Forget()
}

// Forget throws away the GOP and the pending clip, and starts storing again at
// the next I frame, on the same connection. It's for when the clipper stopped
// seeing frames that existed - detection was off for a while - and its GOP
// has a hole: a clip with a missing frame decodes as a smeared image.
func (r *Clipper) Forget() {
	r.current = r.current[:0]
	r.storing = false
	r.drop()
}

// Close ends the connection and returns the clip that was waiting for the
// window, if there was already something to infer. The window won't close on
// its own - the Method gets a Reset on reconnection -, and the video it had is
// still good.
func (r *Clipper) Close() (Clip, bool) {
	p, ok := r.cut()
	// Without an init, no I frame opens the GOP again: whatever arrives until
	// the next Init belongs to no connection.
	r.init, r.storing = nil, false
	return p, ok
}

// Frame stores a video frame and handles the Verdict the Method gave it, in
// the order the Verdict prescribes. It returns the clip when an onset's
// window closes.
//
// `moof` and `mdat` are the fragment's two boxes, as they go to disk.
func (r *Clipper) Frame(v Verdict, atMs int64, keyframe bool, moof, mdat []byte) (p Clip, ok bool) {
	if v.Cut {
		p, ok = r.cut()
	}

	if keyframe {
		if r.pending && r.candidateEnd > 0 && !r.candInSpare {
			r.current, r.spare = r.spare, r.current
			r.candInSpare = true
		}
		r.current = r.current[:0]
		r.storing = r.init != nil
	}

	stored := false
	if r.storing {
		if len(r.current)+len(moof)+len(mdat) > MaxGOPBytes {
			// The rest of this GOP doesn't fit: it isn't stored, and none of
			// its frames becomes a candidate. What was already marked still
			// stands.
			r.storing = false
		} else {
			r.current = append(r.current, moof...)
			r.current = append(r.current, mdat...)
			stored = true
		}
	}

	if v.Onset {
		// A pending onset that never got a Cut - the method was switched in the
		// middle of the window - is replaced, not cut.
		r.drop()
		r.pending, r.onsetMs = true, atMs
	}
	if v.Candidate && r.pending && stored {
		r.candidateMs, r.candInSpare, r.candidateEnd = atMs, false, len(r.current)
	}
	return p, ok
}

// cut builds the pending clip: the init, and the candidate's buffer up to it.
func (r *Clipper) cut() (Clip, bool) {
	if !r.pending {
		return Clip{}, false
	}
	if r.candidateEnd == 0 {
		r.drop()
		return Clip{}, false
	}
	r.pending = false
	buf := r.current
	if r.candInSpare {
		buf = r.spare
	}
	out := make([]byte, 0, len(r.init)+r.candidateEnd)
	out = append(out, r.init...)
	out = append(out, buf[:r.candidateEnd]...)
	return Clip{Camera: r.camera, OnsetMs: r.onsetMs, FrameMs: r.candidateMs, Fmp4: out}, true
}
