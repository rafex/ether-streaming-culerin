package ffmpeg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	flvTagAudio  = 8
	flvTagVideo  = 9
	flvTagScript = 18
)

// splicer joins the FLV outputs of successive compositor runs into one
// continuous stream for the long-lived publisher process, so the RTMP(S)
// connections survive layout changes (which restart the compositor).
//
// Runs overlap (make-before-break): a newer run reads in the background and
// takes over when it produces its first media tag; the older run is then
// dropped. Each run starts its timestamps at zero; the splicer rewrites them
// onto one timeline and drops repeated headers, metadata and identical codec
// sequence headers.
type splicer struct {
	mu      sync.Mutex
	w       io.Writer
	started time.Time
	now     func() time.Time
	active  int // generation allowed to write

	headerSent bool
	metaSent   bool
	last       map[byte]uint32 // last written timestamp per tag type
	seq        map[byte][]byte // last sequence header per tag type
}

func newSplicer(w io.Writer, now func() time.Time) *splicer {
	return &splicer{w: w, now: now, started: now(), active: -1, last: map[byte]uint32{}, seq: map[byte][]byte{}}
}

// errSuperseded ends the feed of a run replaced by a newer one.
var errSuperseded = errors.New("compositor run superseded")

// feed copies the run gen until r ends or a newer run takes over. onAir is
// called once, when this run becomes the one being published.
func (s *splicer) feed(gen int, r io.Reader, onAir func()) error {
	header := make([]byte, 13) // 9-byte header + PreviousTagSize0
	if _, err := io.ReadFull(r, header); err != nil {
		return eofOK(err)
	}
	if !bytes.Equal(header[:3], []byte("FLV")) {
		return fmt.Errorf("compositor output is not FLV")
	}

	var offset int64
	onAirNow := false
	var pendingMeta []byte
	tagHeader := make([]byte, 11)
	for {
		if _, err := io.ReadFull(r, tagHeader); err != nil {
			return eofOK(err)
		}
		kind := tagHeader[0] & 0x1f
		size := int(tagHeader[1])<<16 | int(tagHeader[2])<<8 | int(tagHeader[3])
		ts := uint32(tagHeader[4])<<16 | uint32(tagHeader[5])<<8 | uint32(tagHeader[6]) | uint32(tagHeader[7])<<24
		body := make([]byte, size+4) // data + PreviousTagSize
		if _, err := io.ReadFull(r, body); err != nil {
			return eofOK(err)
		}
		data := body[:size]
		if kind == flvTagScript {
			if pendingMeta == nil {
				pendingMeta = append(append([]byte(nil), tagHeader...), body...)
			}
			continue
		}
		if kind != flvTagAudio && kind != flvTagVideo {
			continue
		}

		s.mu.Lock()
		if s.active > gen {
			s.mu.Unlock()
			return errSuperseded
		}
		if !onAirNow {
			// Take over: place this run on the shared timeline (wall clock since
			// the first frame, never behind what was already written).
			s.active = gen
			onAirNow = true
			if err := s.writeHeaderLocked(header, pendingMeta); err != nil {
				s.mu.Unlock()
				return err
			}
			if len(s.last) == 0 {
				s.started = s.now()
			}
			target := s.now().Sub(s.started).Milliseconds()
			for _, last := range s.last {
				target = max(target, int64(last)+1)
			}
			offset = target - int64(ts)
			onAir()
		}
		err := s.writeTagLocked(kind, tagHeader, body, data, int64(ts)+offset)
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
}

func (s *splicer) writeHeaderLocked(header, meta []byte) error {
	if !s.headerSent {
		header[4] = 0x05 // audio + video
		if _, err := s.w.Write(header); err != nil {
			return err
		}
		s.headerSent = true
	}
	if !s.metaSent && meta != nil {
		writeTimestamp(meta, 0)
		if _, err := s.w.Write(meta); err != nil {
			return err
		}
		s.metaSent = true
	}
	return nil
}

func (s *splicer) writeTagLocked(kind byte, tagHeader, body, data []byte, out int64) error {
	if isSequenceHeader(kind, data) {
		if prev, ok := s.seq[kind]; ok && bytes.Equal(prev, data) {
			return nil
		}
		s.seq[kind] = append([]byte(nil), data...)
	}
	if last, ok := s.last[kind]; ok && out < int64(last) {
		out = int64(last)
	}
	out = max(out, 0)
	s.last[kind] = uint32(out)
	writeTimestamp(tagHeader, uint32(out))
	if _, err := s.w.Write(tagHeader); err != nil {
		return err
	}
	size := len(data)
	binary.BigEndian.PutUint32(body[size:], uint32(11+size))
	_, err := s.w.Write(body)
	return err
}

func writeTimestamp(tagHeader []byte, ts uint32) {
	tagHeader[4] = byte(ts >> 16)
	tagHeader[5] = byte(ts >> 8)
	tagHeader[6] = byte(ts)
	tagHeader[7] = byte(ts >> 24)
}

// isSequenceHeader detects AVC decoder configuration and AAC
// AudioSpecificConfig packets.
func isSequenceHeader(kind byte, data []byte) bool {
	if len(data) < 2 {
		return false
	}
	switch kind {
	case flvTagVideo:
		return data[0]&0x0f == 7 && data[1] == 0
	case flvTagAudio:
		return data[0]>>4 == 10 && data[1] == 0
	}
	return false
}

func eofOK(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	return err
}
