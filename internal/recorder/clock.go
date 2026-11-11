package recorder

// mediaClock estimates at which wall-clock instant each frame of a connection
// was delivered, matching the camera's media clock (tfdt) with the time the
// fragments arrive.
//
// Neither works on its own. Arrival swings with the network, which delivers
// in bursts - measured at ±1.1s in a 30s segment. The media clock is stable,
// but ticks at the pace of the camera's crystal, not of the real clock: one
// camera runs 0.04% fast, and on 09/11/2026 its timeline got to show 21.8s
// more than the time printed on the image.
//
// The difference arrival - media is the delivery delay plus a constant that
// drifts slowly. The delay is never negative, so the SMALLEST value seen in a
// window is the frame that arrived with the minimum delay: it's the estimate
// a burst doesn't contaminate, because a burst only delays. The window slides
// in buckets to follow the drift - a minimum over the whole connection would
// be stuck in the past when the camera runs slower than the real clock.
type mediaClock struct {
	bucket int64 // how many ms of media each bucket covers

	start int64 // media (ms) at which the current bucket started
	cur   int64 // smallest arrival - media of the current bucket
	prev  int64 // the same, for the bucket that just closed

	hasCur, hasPrev bool
}

// observe records a fragment: its media instant and the time it arrived, both
// in ms.
func (c *mediaClock) observe(mediaMs, arrivalMs int64) {
	d := arrivalMs - mediaMs
	switch {
	case !c.hasCur:
		c.start, c.cur, c.hasCur = mediaMs, d, true
	case mediaMs-c.start >= c.bucket:
		c.prev, c.hasPrev = c.cur, true
		c.start, c.cur = mediaMs, d
	default:
		c.cur = min(c.cur, d)
	}
}

// wall translates a media instant to the wall clock. The false only comes
// before the first observation.
//
// It looks at both buckets: the current one may have just opened with a single
// frame, and a single frame is exactly the measure a burst contaminates.
func (c *mediaClock) wall(mediaMs int64) (int64, bool) {
	if !c.hasCur {
		return 0, false
	}
	d := c.cur
	if c.hasPrev {
		d = min(d, c.prev)
	}
	return mediaMs + d, true
}

// splicedStart decides where, on the timeline, a segment that continues the
// previous one on the same connection starts.
//
// Within a connection the media is continuous, so the natural place is the
// splice - the end of the previous segment. But ALWAYS anchoring there
// accumulates the pace difference between the camera and the real clock,
// segment after segment, until the next reconnection. So the splice moves
// toward the estimated clock (target) when the difference goes past the
// tolerance.
//
// The step is half a frame per segment, and that size is what makes it free.
// Backwards, the new segment overlaps less than a frame of the previous one,
// and MSE only drops an overlapped frame when the new one starts at the same
// instant - here no frame is lost. Forwards, it opens a gap smaller than a
// frame, which the browser bridges without stopping playback. Half a frame
// every 30s corrects up to 0.1% at 15fps, plenty of room over the measured
// 0.04%.
//
// The exception is a timeline lagging beyond `gap`: then the media stopped
// counting time that actually passed - the camera froze and resumed without
// jumping its clock - and the honest thing is to show a gap, like any other
// missing recording. A slow camera's lag never gets there, because the
// half-frame steps correct it first. Ahead, there's nothing to skip: jumping
// back would overlap recordings, so it keeps correcting half a frame at a
// time.
func splicedStart(splice, target, halfFrame, tolerance, gap int64) int64 {
	delay := target - splice // positive: the timeline is behind the clock
	switch {
	case delay > gap:
		return target
	case delay > tolerance:
		return splice + halfFrame
	case delay < -tolerance:
		return splice - halfFrame
	default:
		return splice
	}
}
