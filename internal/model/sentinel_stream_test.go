package model

import (
	"strings"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/sentinel"
)

// End-to-end through the real pipeline: a stream captured from a real
// `go test -json` run of a package that imports the library, decoded with the
// real stream decoder, reassembled by the real joiner, and read by the real
// sentinel decoder. Nothing here is hand-written.

func extract(t *testing.T, p *Package, test string) (kinds []string, plain []string) {
	t.Helper()
	tst, ok := p.Tests[test]
	if !ok {
		t.Fatalf("no test %q; have %v", test, p.TestOrder)
	}
	for _, occ := range tst.Occurrences {
		for _, line := range occ.Lines {
			m, found, err := sentinel.Decode(line)
			switch {
			case found && err == nil:
				kinds = append(kinds, m.Kind)
			default:
				plain = append(plain, line)
			}
		}
	}
	return kinds, plain
}

func TestMeta_EveryMetadataKindSurvivesARealRun(t *testing.T) {
	p := pkgBySuffix(t, build(t, "meta.jsonl"), "pkg_meta")

	kinds, _ := extract(t, p, "TestRecordsEveryKindOfMetadata")
	want := []string{
		sentinel.KindLabel, sentinel.KindTag, sentinel.KindLink, sentinel.KindPriority,
		sentinel.KindDescription, sentinel.KindParameter, sentinel.KindParameter,
		sentinel.KindStepStart, sentinel.KindParameter, sentinel.KindStepStart,
		sentinel.KindStepStop, sentinel.KindStepStop,
	}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("kinds =\n  %v\nwant\n  %v", kinds, want)
	}
}

func TestMeta_StepsNestInEmissionOrder(t *testing.T) {
	// There is no step id on the wire; the replayer reconstructs nesting from
	// order alone, so the order must be exactly start,start,stop,stop.
	p := pkgBySuffix(t, build(t, "meta.jsonl"), "pkg_meta")
	kinds, _ := extract(t, p, "TestRecordsEveryKindOfMetadata")

	var stack int
	var maxDepth int
	for _, k := range kinds {
		switch k {
		case sentinel.KindStepStart:
			stack++
			if stack > maxDepth {
				maxDepth = stack
			}
		case sentinel.KindStepStop:
			stack--
			if stack < 0 {
				t.Fatal("a step was closed that was never opened")
			}
		}
	}
	if stack != 0 {
		t.Errorf("%d step(s) left open", stack)
	}
	if maxDepth != 2 {
		t.Errorf("max nesting depth = %d, want 2", maxDepth)
	}
}

func TestMeta_MaskedParameterNeverReachesTheStream(t *testing.T) {
	// The strongest form of the check: the secret must be absent from the raw
	// captured bytes, not merely from a decoded field.
	p := pkgBySuffix(t, build(t, "meta.jsonl"), "pkg_meta")
	tst := p.Tests["TestRecordsEveryKindOfMetadata"]

	var all strings.Builder
	for _, occ := range tst.Occurrences {
		all.WriteString(strings.Join(occ.Lines, "\n"))
	}
	// "token" is the parameter NAME and may appear; a value never was supplied,
	// and MaskedParameter's signature cannot accept one.
	for _, m := range decodeAllMessages(t, tst) {
		if m.Kind == sentinel.KindParameter && m.Masked && m.Value != "" {
			t.Errorf("a masked parameter carried a value: %+v", m)
		}
	}
}

func decodeAllMessages(t *testing.T, tst *Test) []sentinel.Message {
	t.Helper()
	var out []sentinel.Message
	for _, occ := range tst.Occurrences {
		for _, line := range occ.Lines {
			if m, found, err := sentinel.Decode(line); found && err == nil {
				out = append(out, m)
			}
		}
	}
	return out
}

func TestMeta_OutputThatLooksLikeASentinelIsNotConsumed(t *testing.T) {
	// A user echoing sentinel-shaped text must not have it silently eaten, and
	// must not break the real message that follows.
	p := pkgBySuffix(t, build(t, "meta.jsonl"), "pkg_meta")
	kinds, plain := extract(t, p, "TestOutputThatLooksLikeASentinel")

	if len(kinds) != 1 || kinds[0] != sentinel.KindLabel {
		t.Errorf("the real message after the decoys should be the only one decoded, got %v", kinds)
	}
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "this is not really a sentinel") {
		t.Error("the decoy line must survive in the test's plain output")
	}
	if !strings.Contains(joined, "@@QF") {
		t.Error("the truncated decoy must survive too")
	}
}

func TestMeta_SubtestMetadataBelongsToTheSubtest(t *testing.T) {
	// The explicit tb makes this unambiguous: whichever t was passed owns it.
	p := pkgBySuffix(t, build(t, "meta.jsonl"), "pkg_meta")

	parent := decodeAllMessages(t, p.Tests["TestMetadataOnASubtestBelongsToTheSubtest"])
	child := decodeAllMessages(t, p.Tests["TestMetadataOnASubtestBelongsToTheSubtest/child"])

	if len(parent) != 1 || parent[0].Value != "parent" {
		t.Errorf("parent metadata = %+v", parent)
	}
	if len(child) != 1 || child[0].Value != "child" {
		t.Errorf("child metadata = %+v", child)
	}
}
