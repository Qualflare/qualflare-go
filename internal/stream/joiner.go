package stream

import "strings"

// Joiner reassembles logical lines from output chunks.
//
// `go doc cmd/test2json` defines Output as "a portion of the test's output",
// with the guarantee only that concatenating every Output field reproduces the
// exact output. test2json's input buffer is 4096 bytes and its output buffer
// 1024, so a long line arrives as SEVERAL output events. Treating one event as
// one line works for every short log and fails precisely on the large payloads
// that matter -- which is why this exists.
//
// Buffering is keyed on (package, test), never on package alone. Under
// -parallel two tests' chunks interleave, and a package-keyed buffer splices
// half-lines from different tests together, producing corrupt payloads that
// look like flaky metadata loss.
type Joiner struct {
	partial map[key]*strings.Builder
	order   []key
}

type key struct {
	pkg  string
	test string
}

func NewJoiner() *Joiner {
	return &Joiner{partial: map[key]*strings.Builder{}}
}

// Add feeds one chunk and returns every logical line it completed. A chunk that
// does not end a line returns nothing and is held until one does.
func (j *Joiner) Add(pkg, test, chunk string) []string {
	if chunk == "" {
		return nil
	}
	k := key{pkg, test}
	b, ok := j.partial[k]
	if !ok {
		b = &strings.Builder{}
		j.partial[k] = b
		j.order = append(j.order, k)
	}
	b.WriteString(chunk)

	buffered := b.String()
	idx := strings.LastIndexByte(buffered, '\n')
	if idx < 0 {
		return nil
	}

	complete := buffered[:idx]
	b.Reset()
	b.WriteString(buffered[idx+1:])

	return strings.Split(complete, "\n")
}

// Flush returns any trailing text never terminated by a newline, in first-seen
// order so output is deterministic. A test binary killed mid-write leaves one;
// dropping it would discard the last thing a panicking test said.
func (j *Joiner) Flush() []Remainder {
	var out []Remainder
	for _, k := range j.order {
		b := j.partial[k]
		if b == nil || b.Len() == 0 {
			continue
		}
		out = append(out, Remainder{Package: k.pkg, Test: k.test, Text: b.String()})
		b.Reset()
	}
	return out
}

// Remainder is unterminated trailing output for one test.
type Remainder struct {
	Package string
	Test    string
	Text    string
}
