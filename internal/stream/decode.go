package stream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
)

// MaxLineBytes bounds one input line. Beyond it the line is truncated and the
// remainder discarded, rather than growing without limit or aborting the run.
const MaxLineBytes = 16 << 20

// MaxPreambleBytes bounds how much non-JSON text is retained for diagnostics.
const MaxPreambleBytes = 8 << 10

// Stats records everything the decoder had to tolerate. A caller that ignores
// these is choosing to upload a report that may be missing the only evidence of
// a failure, so they are surfaced in the report's metadata.
type Stats struct {
	UnparsedLines  int
	OversizedLines int
	UnknownActions map[string]int
}

// Decoder reads newline-delimited events.
type Decoder struct {
	r        *bufio.Reader
	stats    Stats
	preamble bytes.Buffer
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{
		r:     bufio.NewReaderSize(r, 64<<10),
		stats: Stats{UnknownActions: map[string]int{}},
	}
}

func (d *Decoder) Stats() Stats { return d.stats }

// Preamble returns the retained non-JSON text, which is where a pre-Go-1.24
// toolchain writes compiler errors.
func (d *Decoder) Preamble() string { return d.preamble.String() }

// Next returns the next event, or io.EOF when the stream is exhausted.
// Non-JSON and oversized lines are recorded and skipped, never fatal.
func (d *Decoder) Next() (Event, error) {
	for {
		line, err := d.readLine()
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if ev, ok := d.decode(line); ok {
				return ev, nil
			}
		}
		if err != nil {
			return Event{}, err
		}
	}
}

func (d *Decoder) decode(line []byte) (Event, bool) {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) == 0 {
		return Event{}, false
	}
	if trimmed[0] != '{' {
		d.recordUnparsed(line)
		return Event{}, false
	}
	var ev Event
	if err := json.Unmarshal(trimmed, &ev); err != nil {
		d.recordUnparsed(line)
		return Event{}, false
	}
	if !knownActions[ev.Action] {
		d.stats.UnknownActions[ev.Action]++
		return Event{}, false
	}
	return ev, true
}

func (d *Decoder) recordUnparsed(line []byte) {
	d.stats.UnparsedLines++
	if remaining := MaxPreambleBytes - d.preamble.Len(); remaining > 0 {
		if len(line) > remaining {
			line = line[:remaining]
		}
		d.preamble.Write(line)
		d.preamble.WriteByte('\n')
	}
}

// readLine returns one line, capped at MaxLineBytes. Beyond the cap the rest of
// the line is consumed and discarded so decoding resynchronises on the next
// newline instead of misreading the tail as a fresh line.
func (d *Decoder) readLine() ([]byte, error) {
	var buf []byte
	truncated := false
	for {
		chunk, err := d.r.ReadSlice('\n')
		if len(chunk) > 0 {
			if !truncated {
				if len(buf)+len(chunk) > MaxLineBytes {
					buf = append(buf, chunk[:MaxLineBytes-len(buf)]...)
					truncated = true
					d.stats.OversizedLines++
				} else {
					buf = append(buf, chunk...)
				}
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return buf, err
	}
}
