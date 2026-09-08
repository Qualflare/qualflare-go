package stream

import (
	"strings"
	"testing"
)

func TestJoiner_OneChunkOneLine(t *testing.T) {
	j := NewJoiner()
	if got := j.Add("p", "TestA", "hello\n"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestJoiner_ReassemblesALineSplitAcrossChunks(t *testing.T) {
	// The behaviour that exists because Output is documented as "a portion of
	// the test's output", not a line.
	j := NewJoiner()
	if got := j.Add("p", "TestA", "he"); got != nil {
		t.Fatalf("a partial chunk must yield nothing, got %q", got)
	}
	if got := j.Add("p", "TestA", "ll"); got != nil {
		t.Fatalf("still partial, got %q", got)
	}
	got := j.Add("p", "TestA", "o world\n")
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("got %q, want [hello world]", got)
	}
}

func TestJoiner_SplitsMultipleLinesInOneChunk(t *testing.T) {
	j := NewJoiner()
	got := j.Add("p", "TestA", "one\ntwo\nthree\n")
	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Fatalf("got %q", got)
	}
}

func TestJoiner_HoldsTheTailAfterTheLastNewline(t *testing.T) {
	j := NewJoiner()
	got := j.Add("p", "TestA", "one\npart")
	if len(got) != 1 || got[0] != "one" {
		t.Fatalf("got %q", got)
	}
	got = j.Add("p", "TestA", "ial\n")
	if len(got) != 1 || got[0] != "partial" {
		t.Fatalf("got %q, want [partial]", got)
	}
}

func TestJoiner_ParallelTestsDoNotSpliceIntoEachOther(t *testing.T) {
	// THE reason the buffer is keyed on (package, test). Under -parallel two
	// tests' chunks interleave; a package-keyed buffer would splice these two
	// half-lines into one corrupt line, which is exactly how a large metadata
	// payload would be silently destroyed.
	j := NewJoiner()
	j.Add("p", "TestA", "aaa")
	j.Add("p", "TestB", "bbb")
	gotA := j.Add("p", "TestA", "AAA\n")
	gotB := j.Add("p", "TestB", "BBB\n")

	if len(gotA) != 1 || gotA[0] != "aaaAAA" {
		t.Errorf("TestA reassembled as %q, want [aaaAAA]", gotA)
	}
	if len(gotB) != 1 || gotB[0] != "bbbBBB" {
		t.Errorf("TestB reassembled as %q, want [bbbBBB]", gotB)
	}
}

func TestJoiner_SamePackageAndTestNameInDifferentPackagesAreSeparate(t *testing.T) {
	j := NewJoiner()
	j.Add("pkg1", "TestA", "one")
	j.Add("pkg2", "TestA", "two")
	got1 := j.Add("pkg1", "TestA", "!\n")
	got2 := j.Add("pkg2", "TestA", "!\n")
	if got1[0] != "one!" || got2[0] != "two!" {
		t.Errorf("cross-package splice: %q / %q", got1, got2)
	}
}

func TestJoiner_PackageLevelOutputIsItsOwnBuffer(t *testing.T) {
	// Package-level events have no Test; that output must not merge with a
	// test's.
	j := NewJoiner()
	j.Add("p", "", "pkg")
	j.Add("p", "TestA", "test")
	gotPkg := j.Add("p", "", "-line\n")
	gotTest := j.Add("p", "TestA", "-line\n")
	if gotPkg[0] != "pkg-line" || gotTest[0] != "test-line" {
		t.Errorf("package and test output merged: %q / %q", gotPkg, gotTest)
	}
}

func TestJoiner_FlushReturnsUnterminatedTail(t *testing.T) {
	// A panicking test's last words often arrive without a trailing newline.
	j := NewJoiner()
	j.Add("p", "TestA", "panic: boom")
	rem := j.Flush()
	if len(rem) != 1 || rem[0].Text != "panic: boom" || rem[0].Test != "TestA" {
		t.Fatalf("Flush = %+v", rem)
	}
	if len(j.Flush()) != 0 {
		t.Error("a second Flush must not repeat the remainder")
	}
}

func TestJoiner_FlushIsDeterministicallyOrdered(t *testing.T) {
	j := NewJoiner()
	for _, name := range []string{"TestC", "TestA", "TestB"} {
		j.Add("p", name, "tail")
	}
	rem := j.Flush()
	if len(rem) != 3 {
		t.Fatalf("expected 3 remainders, got %d", len(rem))
	}
	if rem[0].Test != "TestC" || rem[1].Test != "TestA" || rem[2].Test != "TestB" {
		t.Errorf("Flush order is not first-seen: %v %v %v", rem[0].Test, rem[1].Test, rem[2].Test)
	}
}

func TestJoiner_HandlesAPayloadLargerThanTest2jsonBuffers(t *testing.T) {
	// test2json's input buffer is 4096 and its output buffer 1024, so a large
	// payload genuinely arrives in ~1 KB pieces. This is the shape a 100 KB
	// attachment takes on the wire.
	payload := strings.Repeat("Q", 100_000)
	j := NewJoiner()
	var got []string
	for i := 0; i < len(payload); i += 1024 {
		end := i + 1024
		if end > len(payload) {
			end = len(payload)
		}
		got = append(got, j.Add("p", "TestA", payload[i:end])...)
	}
	if len(got) != 0 {
		t.Fatalf("nothing should complete before the newline, got %d lines", len(got))
	}
	got = j.Add("p", "TestA", "\n")
	if len(got) != 1 || got[0] != payload {
		t.Fatalf("reassembled %d bytes, want %d", len(got[0]), len(payload))
	}
}
