// Package fmp4 reads the box structure of a fragmented MP4 without decoding
// media. dwnvr never touches the video bytes: it only needs to know where each
// fragment starts, whether it carries a keyframe, and its decode time -
// enough to cut segments and build an index.
package fmp4

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// MaxBoxSize caps the size of an accepted box. Without that ceiling, a
// corrupted stream (or a hostile server) would make the recorder allocate
// without limit on a device with 1.5 GB of RAM in total.
const MaxBoxSize = 32 << 20

// Reader reads top-level boxes of an fMP4 coming from a continuous stream.
type Reader struct {
	br  *bufio.Reader
	buf []byte
}

func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 64<<10)}
}

// NextBox reads the next top-level box and returns its type and the full
// bytes, header included.
//
// The returned slice is reused across calls: the caller must write or copy it
// before calling NextBox again. That avoids one allocation per frame - with 9
// cameras at 15 fps that would be 135 needless allocations per second.
func (r *Reader) NextBox() (string, []byte, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r.br, hdr[:8]); err != nil {
		return "", nil, err
	}

	size := int64(binary.BigEndian.Uint32(hdr[0:4]))
	typ := string(hdr[4:8])
	hdrLen := int64(8)

	switch size {
	case 1:
		// 64-bit extended size in the next 8 bytes.
		if _, err := io.ReadFull(r.br, hdr[8:16]); err != nil {
			return "", nil, err
		}
		size = int64(binary.BigEndian.Uint64(hdr[8:16]))
		hdrLen = 16
	case 0:
		// "runs to the end of the file": impossible to delimit in a stream.
		return "", nil, fmt.Errorf("fmp4: box %q with size 0 is not supported in a stream", typ)
	}

	if size < hdrLen || size > MaxBoxSize {
		return "", nil, fmt.Errorf("fmp4: invalid size %d for box %q", size, typ)
	}

	if int64(cap(r.buf)) < size {
		r.buf = make([]byte, size)
	}
	b := r.buf[:size]
	copy(b, hdr[:hdrLen])

	if _, err := io.ReadFull(r.br, b[hdrLen:]); err != nil {
		return "", nil, err
	}
	return typ, b, nil
}

// boxHeaderLen returns the header length and the total size of a box that is
// already fully in memory.
func boxHeaderLen(b []byte) (hdrLen, size int, ok bool) {
	if len(b) < 8 {
		return 0, 0, false
	}
	size = int(binary.BigEndian.Uint32(b[0:4]))
	hdrLen = 8
	if size == 1 {
		if len(b) < 16 {
			return 0, 0, false
		}
		size = int(binary.BigEndian.Uint64(b[8:16]))
		hdrLen = 16
	}
	if size < hdrLen || size > len(b) {
		return 0, 0, false
	}
	return hdrLen, size, true
}

// boxType returns the type of a box that is already in memory.
func boxType(b []byte) string {
	if len(b) < 8 {
		return ""
	}
	return string(b[4:8])
}

// BoxPayload returns a box's content without the header, honoring the 16-byte
// extended header. A large mdat uses that form, and blindly assuming 8 bytes
// would start reading in the middle of a field.
func BoxPayload(b []byte) []byte {
	hdrLen, size, ok := boxHeaderLen(b)
	if !ok {
		return nil
	}
	return b[hdrLen:size]
}

// walk goes through the child boxes contained in payload, calling fn for each
// one with its body (without the header). Returning errStopWalk stops early.
func walk(payload []byte, fn func(typ string, body []byte) error) error {
	for off := 0; off+8 <= len(payload); {
		hdrLen, size, ok := boxHeaderLen(payload[off:])
		if !ok {
			return fmt.Errorf("fmp4: malformed box at offset %d", off)
		}
		typ := boxType(payload[off:])
		if err := fn(typ, payload[off+hdrLen:off+size]); err != nil {
			return err
		}
		off += size
	}
	return nil
}

// be16, be32 and be64 read big-endian integers with bounds checks, so a
// truncated stream becomes an error instead of a panic.
func be16(b []byte, off int) (uint16, bool) {
	if off < 0 || off+2 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint16(b[off : off+2]), true
}

func be32(b []byte, off int) (uint32, bool) {
	if off < 0 || off+4 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint32(b[off : off+4]), true
}

func be64(b []byte, off int) (uint64, bool) {
	if off < 0 || off+8 > len(b) {
		return 0, false
	}
	return binary.BigEndian.Uint64(b[off : off+8]), true
}
