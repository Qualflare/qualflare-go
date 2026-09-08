package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run() is the whole binary minus os.Exit, so these drive the real thing:
// flag parsing, mode selection, report writing and exit-code propagation.

// run2 keeps the existing tests terse: they assert on diagnostics, so stdout is
// discarded and errOut is the buffer they inspect.
func run2(argv []string, stdin io.Reader, errOut io.Writer) int {
	return run(argv, stdin, io.Discard, errOut)
}

func readReport(t *testing.T, dir string) map[string]any {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("expected exactly one report in %s, found %d", dir, len(files))
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	m["__file"] = filepath.Base(files[0])
	return m
}

func cases(m map[string]any) []map[string]any {
	var out []map[string]any
	for _, s := range m["suites"].([]any) {
		for _, c := range s.(map[string]any)["cases"].([]any) {
			out = append(out, c.(map[string]any))
		}
	}
	return out
}

const passStream = `{"Action":"start","Package":"m"}
{"Action":"run","Package":"m","Test":"TestA"}
{"Action":"pass","Package":"m","Test":"TestA","Elapsed":0.5}
{"Action":"pass","Package":"m","Elapsed":0.6}
`

func writeStream(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stream.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"QUALFLARE_OUTPUT_DIR", "QUALFLARE_ENVIRONMENT", "QUALFLARE_ENABLED",
		"QUALFLARE_RUN_ID", "QUALFLARE_REPEAT", "QUALFLARE_BRANCH", "QUALFLARE_COMMIT",
		"GITHUB_ACTIONS", "GITHUB_REF_NAME", "GITHUB_SHA", "GITHUB_RUN_ID",
	} {
		t.Setenv(k, "")
	}
}

func TestVersionGoesToStdoutNotStderr(t *testing.T) {
	// A release workflow reads this with $(qualflare-go -version). Printing it
	// to stderr makes it invisible there -- which is exactly how the first
	// v0.1.0 release attempt failed.
	cleanEnv(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"-version"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "qualflare-go") {
		t.Errorf("stdout = %q, want the version", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr should be empty, got %q", errOut.String())
	}
}

func TestFileMode_WritesAReport(t *testing.T) {
	cleanEnv(t)
	dir := t.TempDir()
	var out bytes.Buffer
	code := run2([]string{"-i", writeStream(t, passStream), "--output-dir", dir}, strings.NewReader(""), &out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	m := readReport(t, dir)
	if m["framework"] != "golang" {
		t.Errorf("framework = %v", m["framework"])
	}
	if got := cases(m); len(got) != 1 || got[0]["name"] != "TestA" {
		t.Errorf("cases = %v", got)
	}
}

func TestPipeMode_WritesAReportAndWarns(t *testing.T) {
	// Pipe mode cannot see the exit code or stderr, so it must say so rather
	// than let a user believe a build failure would be caught.
	cleanEnv(t)
	dir := t.TempDir()
	var out bytes.Buffer
	code := run2([]string{"--output-dir", dir}, strings.NewReader(passStream), &out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	if len(cases(readReport(t, dir))) != 1 {
		t.Error("expected one case from the piped stream")
	}
	if !strings.Contains(out.String(), "stdin") {
		t.Errorf("pipe mode should warn about its limitation, got %q", out.String())
	}
}

func TestReportFilenameSaysGolangNotGo(t *testing.T) {
	// Content detection matches the framework+metadata+suites triple first, but
	// if it ever fails, qualflare-cli's filename fallback matches a
	// word-boundary "go" token plus .json and routes the file to its NDJSON
	// go-test parser, producing garbage. "golang" makes that fallback error
	// instead. Fail loud, never wrong.
	cleanEnv(t)
	dir := t.TempDir()
	var out bytes.Buffer
	run2([]string{"-i", writeStream(t, passStream), "--output-dir", dir}, strings.NewReader(""), &out)
	name := readReport(t, dir)["__file"].(string)
	if !strings.HasPrefix(name, "qualflare-golang-") {
		t.Errorf("report is named %q; it must start with qualflare-golang-", name)
	}
}

func TestDisabledWritesNothing(t *testing.T) {
	cleanEnv(t)
	dir := t.TempDir()
	var out bytes.Buffer
	if code := run2([]string{"--enabled", "false", "-i", writeStream(t, passStream), "--output-dir", dir},
		strings.NewReader(""), &out); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 0 {
		t.Errorf("expected no report when disabled, found %d", len(files))
	}
}

func TestRepeatedInputsBecomeLaterAttempts(t *testing.T) {
	// Go has no rerun flag, so separate invocations are the only multi-attempt
	// shape that exists. Treating later files as later attempts is an explicit
	// assertion by the user, never an inference across process boundaries.
	cleanEnv(t)
	dir := t.TempDir()
	failStream := strings.Replace(passStream,
		`{"Action":"pass","Package":"m","Test":"TestA","Elapsed":0.5}`,
		`{"Action":"fail","Package":"m","Test":"TestA","Elapsed":0.5}`, 1)
	var out bytes.Buffer
	code := run2([]string{"-i", writeStream(t, failStream), "-i", writeStream(t, passStream), "--output-dir", dir},
		strings.NewReader(""), &out)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, out.String())
	}
	got := cases(readReport(t, dir))
	if len(got) != 1 {
		t.Fatalf("expected one collapsed case, got %d", len(got))
	}
	attempts, _ := got[0]["attempts"].([]any)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %v", got[0]["attempts"])
	}
	if got[0]["status"] != "passed" || got[0]["isFlaky"] != true {
		t.Errorf("status=%v isFlaky=%v -- a run that ended green after failing is flaky", got[0]["status"], got[0]["isFlaky"])
	}
}

func TestMissingInputFileIsAnError(t *testing.T) {
	cleanEnv(t)
	var out bytes.Buffer
	if code := run2([]string{"-i", "/definitely/not/here.json", "--output-dir", t.TempDir()},
		strings.NewReader(""), &out); code == 0 {
		t.Error("expected a non-zero exit for a missing input")
	}
}

func TestUnknownFlagExitsTwo(t *testing.T) {
	cleanEnv(t)
	var out bytes.Buffer
	if code := run2([]string{"--not-a-flag"}, strings.NewReader(""), &out); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestAnomaliesAreReportedOnStderr(t *testing.T) {
	cleanEnv(t)
	dir := t.TempDir()
	var out bytes.Buffer
	body := "this line is not JSON\n" + passStream
	run2([]string{"-i", writeStream(t, body), "--output-dir", dir}, strings.NewReader(""), &out)
	if !strings.Contains(out.String(), "not JSON") {
		t.Errorf("the decoder had to skip a line and should have said so: %q", out.String())
	}
}

func TestGoTestArgv(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []string
		want string
	}{
		{"bare package pattern", []string{"./..."}, "go test -json ./..."},
		{"explicit go test", []string{"go", "test", "./..."}, "go test -json ./..."},
		{"flags are preserved", []string{"-race", "./..."}, "go test -json -race ./..."},
		{"json is not added twice", []string{"go", "test", "-json", "./..."}, "go test -json ./..."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(goTestArgv(tc.in), " "); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- wrapper mode ------------------------------------------------------------

// These exec a real `go test`, which is slow, so they are skipped under -short.
// They are also the only tests that exercise the two signals a pipe cannot see.

func TestWrapper_PropagatesGoTestsExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("execs a real go test")
	}
	cleanEnv(t)
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The fixtures are a module of their own, so go test has to run from
	// inside them -- a relative path from here is "outside the main module".
	cwd, _ := os.Getwd()
	if err := os.Chdir("../../test/integration/fixtures/awkward"); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	var out bytes.Buffer
	code := run2([]string{"--output-dir", dir, "--", "go", "test",
		"-count=1", "-run", "TestPasses", "./pkg_pass/"},
		strings.NewReader(""), &out)
	if code != 0 {
		t.Fatalf("a passing run should exit 0, got %d: %s", code, out.String())
	}
	if len(cases(readReport(t, dir))) == 0 {
		t.Error("expected at least one case")
	}
}

func TestWrapper_ABrokenBuildIsNeverReportedGreen(t *testing.T) {
	// THE property the wrapper exists for. On Go 1.21/1.23 a package that fails
	// to compile produces no fail event at all -- the stream is entirely green
	// while go test exits 1, and the compiler error goes only to stderr. Every
	// event-derived rule misses it, so only the exit code catches it.
	if testing.Short() {
		t.Skip("execs a real go test")
	}
	cleanEnv(t)
	dir, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is a module of its own precisely because it cannot compile;
	// go test has to be run from inside it.
	cwd, _ := os.Getwd()
	if err := os.Chdir("../../test/integration/fixtures/buildfail"); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	var out bytes.Buffer
	code := run2([]string{"--output-dir", dir, "--", "go", "test", "-count=1", "./..."},
		strings.NewReader(""), &out)

	if code == 0 {
		t.Fatal("a broken build must not exit 0")
	}
	var red int
	for _, c := range cases(readReport(t, dir)) {
		if c["status"] != "passed" {
			red++
		}
	}
	if red == 0 {
		t.Fatal("go test failed but every case in the report is green -- the backstop did not fire")
	}
}
