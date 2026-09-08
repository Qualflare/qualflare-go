package sentinel

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	in := Message{
		Kind: KindLabel, Name: "team", Value: "platform",
		Tags: []string{"smoke", "checkout"}, Duration: 1234, Masked: true,
	}
	line, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, found, err := Decode(line)
	if err != nil || !found {
		t.Fatalf("Decode: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Errorf("round trip changed the message: %+v -> %+v", in, out)
	}
}

func TestEncode_ProducesASingleLineWithNoHazardousBytes(t *testing.T) {
	// The property the whole format rests on: it cannot be split, cannot
	// terminate itself, and cannot be reinterpreted by test2json as framing.
	line, err := Encode(Message{Kind: KindDescription, Text: "multi\nline\ttext with \"quotes\" and \x16 marker"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, bad := range []string{"\n", "\r", "\x16", "=== ", "--- "} {
		if strings.Contains(line, bad) {
			t.Errorf("encoded line contains %q: %q", bad, line)
		}
	}
}

func TestDecode_FindsTheMessageDespiteTheTLogPrefix(t *testing.T) {
	// t.Log emits "<indent>file_test.go:12: <text>", so a decoder anchored at
	// offset zero would find nothing at all.
	line, _ := Encode(Message{Kind: KindTag, Tags: []string{"smoke"}})
	prefixed := "    qualflare_test.go:42: " + line
	out, found, err := Decode(prefixed)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if len(out.Tags) != 1 || out.Tags[0] != "smoke" {
		t.Errorf("got %+v", out)
	}
}

func TestDecode_ToleratesTrailingText(t *testing.T) {
	line, _ := Encode(Message{Kind: KindLabel, Name: "a", Value: "b"})
	if _, found, err := Decode(line + " trailing junk"); err != nil || !found {
		t.Errorf("found=%v err=%v", found, err)
	}
}

func TestDecode_OrdinaryOutputIsNotOurs(t *testing.T) {
	for _, line := range []string{
		"", "hello world", "--- FAIL: TestThing (0.00s)",
		"=== RUN   TestThing", "    foo_test.go:1: some log",
		"@@ not a sentinel @@", "@@QFx@@nope@@",
	} {
		m, found, err := Decode(line)
		if found || err != nil {
			t.Errorf("Decode(%q) = %+v found=%v err=%v; want not-ours", line, m, found, err)
		}
	}
}

func TestDecode_LooksOursButIsBrokenIsAnError(t *testing.T) {
	// found=true with an error tells the caller to keep the raw line in the
	// test's output and warn -- never to silently consume it.
	cases := map[string]string{
		"no version delimiter": "@@QF1",
		"truncated payload":    "@@QF1@@abcdef",
		"truncated checksum":   "@@QF1@@abcdef@@1234",
		"malformed checksum":   "@@QF1@@abcdef@@zzzzzzzz@@",
		"checksum mismatch":    "@@QF1@@abcdef@@00000000@@",
	}
	for name, line := range cases {
		_, found, err := Decode(line)
		if !found {
			t.Errorf("%s: should be recognised as ours-but-broken", name)
		}
		if err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDecode_ChecksumRejectsATamperedPayload(t *testing.T) {
	line, _ := Encode(Message{Kind: KindLabel, Name: "team", Value: "platform"})

	// "@@QF1@@BODY@@CRC@@" splits to ["", "QF1", "BODY", "CRC", ""], so the
	// payload is parts[2]. Corrupt one character of it and leave the checksum
	// untouched -- which is precisely the misjoined-reassembly case the CRC
	// exists to catch.
	parts := strings.Split(line, delim)
	if len(parts) != 5 {
		t.Fatalf("unexpected encoded shape: %q -> %v", line, parts)
	}
	body := []byte(parts[2])
	if body[0] == 'A' {
		body[0] = 'B'
	} else {
		body[0] = 'A'
	}
	parts[2] = string(body)
	tampered := strings.Join(parts, delim)

	if _, found, err := Decode(tampered); err == nil {
		t.Errorf("a corrupted payload must not decode (found=%v)", found)
	}
}

func TestDecode_UnknownVersionIsReportedNotGuessedAt(t *testing.T) {
	_, found, err := Decode("@@QF99@@abcdef@@00000000@@")
	if !found {
		t.Fatal("a version-skewed line is still ours")
	}
	if !errors.Is(err, ErrUnknownVersion) {
		t.Errorf("err = %v, want ErrUnknownVersion", err)
	}
}

func TestDecode_UnknownKindIsRejected(t *testing.T) {
	// A structurally perfect line whose kind we do not implement must not be
	// accepted as a valid empty message.
	line, err := Encode(Message{Kind: "kind-from-the-future"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, _, err := Decode(line); err == nil {
		t.Error("an unknown kind must be rejected")
	}
}

func TestEncode_RefusesAnOversizedMessage(t *testing.T) {
	// Never truncate: half a base64 payload is not a usable anything. The
	// caller spills to a file and sends a reference instead.
	_, err := Encode(Message{Kind: KindAttachment, Content: strings.Repeat("A", MaxEncodedBytes*2)})
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestEncode_StaysUnderTest2jsonsInputBuffer(t *testing.T) {
	// 4096 is test2json's inBuffer; staying under it keeps a message to one
	// output event on the happy path.
	if MaxEncodedBytes >= 4096 {
		t.Fatalf("MaxEncodedBytes = %d, must stay under test2json's 4096-byte input buffer", MaxEncodedBytes)
	}
	line, err := Encode(Message{Kind: KindDescription, Text: strings.Repeat("x", 2000)})
	if err != nil {
		t.Fatalf("a 2000-byte description should fit: %v", err)
	}
	if len(line) >= 4096 {
		t.Errorf("encoded length %d exceeds the input buffer", len(line))
	}
}

func TestEncode_MaskedParameterCarriesNoValue(t *testing.T) {
	// Masking happens at the source; the encoded bytes must not contain it.
	line, err := Encode(Message{Kind: KindParameter, Name: "token", Masked: true})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(line, "c3VwZXItc2VjcmV0") { // base64 of "super-secret"
		t.Error("secret leaked into the encoded line")
	}
	out, _, _ := Decode(line)
	if out.Value != "" || !out.Masked {
		t.Errorf("got %+v", out)
	}
}

func FuzzDecode(f *testing.F) {
	line, _ := Encode(Message{Kind: KindLabel, Name: "a", Value: "b"})
	for _, seed := range []string{"", "hello", line, "    x_test.go:1: " + line, "@@QF1@@@@@@", "@@QF"} {
		f.Add(seed)
	}
	// Decode must never panic, whatever it is handed.
	f.Fuzz(func(t *testing.T, s string) {
		_, _, _ = Decode(s)
	})
}
