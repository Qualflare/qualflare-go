// Package sentinel is the wire format carrying metadata from a test process to
// the reporter, and the one contract both halves of this module import.
//
// A test body has no channel to a reporter, so metadata rides t.Log and comes
// back out of the `output` events. That imposes the whole design:
//
//   - It must survive being embedded in a line. t.Log prefixes
//     "<indent>file_test.go:12: ", so a decoder anchored at offset zero finds
//     nothing. The magic is located with strings.Index instead.
//
//   - It must be exactly one line. base64url restricts the payload to
//     [A-Za-z0-9_-], so it cannot contain a newline, cannot contain the
//     delimiter, and cannot terminate itself early.
//
//   - It must be distinguishable from a user's own output. A CRC over the
//     encoded payload rejects both a coincidental match and a misjoined
//     reassembly. All four of magic, base64, CRC and a known kind must hold.
//
//   - It must survive a version skew between the library a user imports and the
//     binary that reads it. The version tag is checked first, and an unknown one
//     is reported rather than guessed at.
//
// Format: @@QF1@@<base64url-nopad(compact JSON)>@@<crc32c, 8 lowercase hex>@@
package sentinel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"
)

const (
	// Version is the format this build emits. It changes only when the payload
	// grammar changes incompatibly.
	Version = 1

	prefix = "@@QF"
	delim  = "@@"

	// MaxEncodedBytes bounds one emitted line. test2json's input buffer is 4096
	// bytes, so staying under it keeps a message to a single output event on the
	// happy path. Anything larger is spilled to a file and referenced instead.
	MaxEncodedBytes = 3500
)

// Message kinds.
const (
	KindLabel       = "label"
	KindLink        = "link"
	KindTag         = "tag"
	KindDescription = "desc"
	KindPriority    = "prio"
	KindParameter   = "param"
	KindStepStart   = "step+"
	KindStepStop    = "step-"
	KindAttachment  = "att"
	KindAttachRef   = "ref"
	KindWarning     = "warn"
)

var knownKinds = map[string]bool{
	KindLabel: true, KindLink: true, KindTag: true, KindDescription: true,
	KindPriority: true, KindParameter: true, KindStepStart: true,
	KindStepStop: true, KindAttachment: true, KindAttachRef: true, KindWarning: true,
}

// Message is one metadata event. Field names are short because every byte
// counts against MaxEncodedBytes.
type Message struct {
	Kind     string   `json:"k"`
	Name     string   `json:"n,omitempty"`
	Value    string   `json:"v,omitempty"`
	Masked   bool     `json:"m,omitempty"`
	Tags     []string `json:"tg,omitempty"`
	URL      string   `json:"u,omitempty"`
	Type     string   `json:"ty,omitempty"`
	Text     string   `json:"tx,omitempty"`
	Status   string   `json:"st,omitempty"`
	Error    string   `json:"er,omitempty"`
	Duration int64    `json:"d,omitempty"`
	MimeType string   `json:"mt,omitempty"`
	Content  string   `json:"c,omitempty"`
	Path     string   `json:"p,omitempty"`
	Size     int64    `json:"sz,omitempty"`
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Encode renders a message as a single line. The result is guaranteed to
// contain no newline, so it can never be split across two output events by
// anything other than test2json's own buffering.
func Encode(m Message) (string, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	sum := crc32.Checksum([]byte(body), castagnoli)
	line := fmt.Sprintf("%s%d%s%s%s%08x%s", prefix, Version, delim, body, delim, sum, delim)
	if len(line) > MaxEncodedBytes {
		return "", ErrTooLarge
	}
	return line, nil
}

// ErrTooLarge means the caller must spill the payload to a file and send a
// reference instead. Never truncate: half a base64 payload is not a usable
// anything.
var ErrTooLarge = errors.New("sentinel: encoded message exceeds the line cap")

// ErrUnknownVersion means the line is ours but was written by a different
// library version. Reported rather than guessed at.
var ErrUnknownVersion = errors.New("sentinel: unknown format version")

// Decode extracts a message from anywhere within a line.
//
// Returns found=false when the line simply is not ours, which is the common
// case and not an error. Returns an error when the line LOOKS like ours but is
// not usable -- the caller should then keep the raw line in the test's output
// and warn, never silently consume it.
func Decode(line string) (m Message, found bool, err error) {
	start := strings.Index(line, prefix)
	if start < 0 {
		return Message{}, false, nil
	}
	rest := line[start+len(prefix):]

	end := strings.Index(rest, delim)
	if end < 0 {
		return Message{}, true, errors.New("sentinel: no version delimiter")
	}
	version, verr := strconv.Atoi(rest[:end])
	if verr != nil {
		return Message{}, false, nil // "@@QFx" is not our prefix at all
	}
	if version != Version {
		return Message{}, true, fmt.Errorf("%w: %d", ErrUnknownVersion, version)
	}
	rest = rest[end+len(delim):]

	bodyEnd := strings.Index(rest, delim)
	if bodyEnd < 0 {
		return Message{}, true, errors.New("sentinel: truncated payload")
	}
	body := rest[:bodyEnd]
	rest = rest[bodyEnd+len(delim):]

	if len(rest) < 8 {
		return Message{}, true, errors.New("sentinel: truncated checksum")
	}
	want, cerr := strconv.ParseUint(rest[:8], 16, 32)
	if cerr != nil {
		return Message{}, true, errors.New("sentinel: malformed checksum")
	}
	if uint32(want) != crc32.Checksum([]byte(body), castagnoli) {
		return Message{}, true, errors.New("sentinel: checksum mismatch")
	}

	raw, derr := base64.RawURLEncoding.DecodeString(body)
	if derr != nil {
		return Message{}, true, fmt.Errorf("sentinel: %w", derr)
	}
	if uerr := json.Unmarshal(raw, &m); uerr != nil {
		return Message{}, true, fmt.Errorf("sentinel: %w", uerr)
	}
	if !knownKinds[m.Kind] {
		return Message{}, true, fmt.Errorf("sentinel: unknown kind %q", m.Kind)
	}
	return m, true, nil
}
