package remote

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Sideband channel identifiers.
const (
	SidebandData     byte = 0x01
	SidebandProgress byte = 0x02
	SidebandError    byte = 0x03
)

// SidebandWriter writes length-prefixed sideband frames.
// Frame format: [4 bytes big-endian length][1 byte channel][payload]
type SidebandWriter struct {
	w io.Writer
}

func NewSidebandWriter(w io.Writer) *SidebandWriter {
	return &SidebandWriter{w: w}
}

func (sw *SidebandWriter) writeFrame(channel byte, data []byte) error {
	frameLen := uint32(1 + len(data)) // channel + payload
	if err := binary.Write(sw.w, binary.BigEndian, frameLen); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if _, err := sw.w.Write([]byte{channel}); err != nil {
		return fmt.Errorf("write channel: %w", err)
	}
	if len(data) > 0 {
		if _, err := sw.w.Write(data); err != nil {
			return fmt.Errorf("write payload: %w", err)
		}
	}
	return nil
}

func (sw *SidebandWriter) WriteData(data []byte) error {
	return sw.writeFrame(SidebandData, data)
}

func (sw *SidebandWriter) WriteProgress(msg string) error {
	return sw.writeFrame(SidebandProgress, []byte(msg))
}

func (sw *SidebandWriter) WriteError(msg string) error {
	return sw.writeFrame(SidebandError, []byte(msg))
}

// SidebandReader reads length-prefixed sideband frames.
type SidebandReader struct {
	r io.Reader
}

func NewSidebandReader(r io.Reader) *SidebandReader {
	return &SidebandReader{r: r}
}

// ReadFrame reads one sideband frame, returning channel and payload.
// Returns io.EOF when no more frames are available.
func (sr *SidebandReader) ReadFrame() (byte, []byte, error) {
	var frameLen uint32
	if err := binary.Read(sr.r, binary.BigEndian, &frameLen); err != nil {
		return 0, nil, err
	}
	if frameLen < 1 {
		return 0, nil, fmt.Errorf("sideband frame too short: %d", frameLen)
	}

	frame := make([]byte, frameLen)
	if _, err := io.ReadFull(sr.r, frame); err != nil {
		return 0, nil, fmt.Errorf("read frame: %w", err)
	}

	channel := frame[0]
	payload := frame[1:]
	return channel, payload, nil
}

// SidebandDataReader presents sideband data frames as a sequential io.Reader,
// discarding progress frames (or forwarding them to a callback).
type SidebandDataReader struct {
	sr         *SidebandReader
	onProgress func(string)
	buf        []byte
	done       bool
}

func NewSidebandDataReader(r io.Reader, onProgress func(string)) *SidebandDataReader {
	return &SidebandDataReader{
		sr:         NewSidebandReader(r),
		onProgress: onProgress,
	}
}

func (dr *SidebandDataReader) Read(p []byte) (int, error) {
	for len(dr.buf) == 0 {
		if dr.done {
			return 0, io.EOF
		}
		channel, payload, err := dr.sr.ReadFrame()
		if err == io.EOF {
			dr.done = true
			return 0, io.EOF
		}
		if err != nil {
			return 0, err
		}
		switch channel {
		case SidebandData:
			dr.buf = payload
		case SidebandProgress:
			if dr.onProgress != nil {
				dr.onProgress(string(payload))
			}
		case SidebandError:
			return 0, fmt.Errorf("remote error: %s", string(payload))
		}
	}

	n := copy(p, dr.buf)
	dr.buf = dr.buf[n:]
	return n, nil
}
