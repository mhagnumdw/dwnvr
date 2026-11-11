package fmp4

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

// InitGen identifies an init segment by the hash of its content.
//
// It's what lets us notice on our own that the codec changed (a different SPS
// after a reconnection) without keeping any counter, and what makes an orphan
// segment recovered at boot point back to the right init.
func InitGen(init []byte) string {
	sum := sha256.Sum256(init)
	return hex.EncodeToString(sum[:6])
}

// SegmentInfo is what dwnvr's index needs to know about a segment already
// written. All of it comes from the box structure, without decoding media.
type SegmentInfo struct {
	// InitSize is the size of ftyp+moov at the start of the file. Delivery via
	// MSE/HLS skips that prefix and serves the init separately, only once.
	InitSize int64
	// FirstFragSize is the size of the first moof+mdat pair. Init + first
	// fragment make a single-frame MP4: that's how dwnvr serves a thumbnail
	// without decoding anything on the server.
	FirstFragSize int64
	// DurationMs is the time the segment covers.
	DurationMs int64
	// Keyframes is how many video keyframes the segment contains.
	Keyframes int
	// Frames is the total number of video fragments.
	Frames int
	// Gen is the hash of the embedded init, the same one the recorder uses.
	Gen   string
	Movie *Movie
}

// ProbeSegment reads the structure of a recorded segment. It's used to rebuild
// the index when it's lost and to check, at boot, that the tail of the index
// matches what's on disk.
func ProbeSegment(path string) (*SegmentInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return probeSegment(f)
}

func probeSegment(r io.Reader) (*SegmentInfo, error) {
	info := &SegmentInfo{}
	rd := NewReader(r)

	var (
		videoTrack   Track
		haveVideo    bool
		offset       int64
		lastVideoEnd uint64
		fragStart    int64 = -1
		init         []byte
	)

	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		n := int64(len(box))

		switch typ {
		case "ftyp":
			info.InitSize += n
			init = append(init, box...)
		case "moov":
			info.InitSize += n
			init = append(init, box...)
			info.Gen = InitGen(init)
			mv, err := ParseMoov(box)
			if err != nil {
				return nil, err
			}
			info.Movie = mv
			videoTrack, haveVideo = mv.VideoTrack()
		case "moof":
			if !haveVideo {
				break
			}
			if fragStart < 0 {
				fragStart = offset
			}
			frag, err := ParseMoof(box, videoTrack.ID)
			if err != nil {
				return nil, err
			}
			if frag.TrackID == videoTrack.ID {
				info.Frames++
				if frag.Keyframe {
					info.Keyframes++
				}
				lastVideoEnd = frag.EndTime()
			}
		case "mdat":
			// The first complete moof+mdat sets the thumbnail size.
			if info.FirstFragSize == 0 && fragStart >= 0 {
				info.FirstFragSize = offset + n - fragStart
			}
		}
		offset += n
	}

	if haveVideo && videoTrack.Timescale > 0 {
		info.DurationMs = int64(float64(lastVideoEnd) / float64(videoTrack.Timescale) * 1000)
	}
	return info, nil
}
