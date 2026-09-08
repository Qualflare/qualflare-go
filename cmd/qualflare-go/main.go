// Command qualflare-go turns a `go test -json` stream into a Qualflare report.
//
//	qualflare-go ./...                      # runs go test for you
//	qualflare-go -- go test -race ./...     # any go test flags
//	go test -json ./... | qualflare-go      # pipe
//	qualflare-go -i results.json            # a saved stream, repeatable
//
// The wrapper forms are the documented ones, and not for convenience: they are
// the only modes that can see `go test`'s exit code and its stderr. On Go before
// 1.24 a build failure produces NO event in the JSON stream at all, so a pipe
// structurally cannot tell a broken build from a green run.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Qualflare/qualflare-go/internal/build"
	"github.com/Qualflare/qualflare-go/internal/config"
	"github.com/Qualflare/qualflare-go/internal/model"
	runnerpkg "github.com/Qualflare/qualflare-go/internal/runner"
	"github.com/Qualflare/qualflare-go/internal/stream"
	"github.com/Qualflare/qualflare-go/internal/version"
	"github.com/Qualflare/qualflare-go/internal/wire"
)

type inputFiles []string

func (f *inputFiles) String() string     { return strings.Join(*f, ",") }
func (f *inputFiles) Set(v string) error { *f = append(*f, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stderr))
}

func run(argv []string, stdin io.Reader, errOut io.Writer) int {
	fs := flag.NewFlagSet("qualflare-go", flag.ContinueOnError)
	fs.SetOutput(errOut)

	var (
		inputs      inputFiles
		showVersion = fs.Bool("version", false, "print the version and exit")
		f           config.Flags
	)
	fs.Var(&inputs, "i", "read a saved `go test -json` stream from this file (repeatable)")
	fs.StringVar(&f.OutputDir, "output-dir", "", "directory to write the report into")
	fs.StringVar(&f.Environment, "environment", "", "environment the run belongs to")
	fs.StringVar(&f.Language, "language", "", "report language")
	fs.StringVar(&f.Framework, "framework", "", "framework name")
	fs.StringVar(&f.Platform, "platform", "", "platform label")
	fs.StringVar(&f.Milestone, "milestone", "", "milestone sequence number")
	fs.StringVar(&f.Branch, "branch", "", "branch name")
	fs.StringVar(&f.Commit, "commit", "", "commit sha")
	fs.StringVar(&f.RunID, "run-id", "", "groups a launch's report files; every shard must share one")
	fs.StringVar(&f.ShardIndex, "shard-index", "", "which shard produced these cases")
	fs.StringVar(&f.Enabled, "enabled", "", "set false to write no report")
	fs.StringVar(&f.Repeat, "repeat", "", "collapse|split -- how to treat -count=N reruns")

	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(errOut, "qualflare-go "+version.Full())
		return 0
	}

	cfg := config.Resolve(f, newRunID)
	if !cfg.Enabled {
		return 0
	}

	rest := fs.Args()
	switch {
	case len(inputs) > 0:
		return fromFiles(inputs, cfg, errOut)
	case len(rest) > 0:
		return wrap(rest, cfg, errOut)
	default:
		return fromPipe(stdin, cfg, errOut)
	}
}

// consume decodes a stream into a run model.
func consume(r io.Reader) (*model.Run, stream.Stats, string) {
	b := model.NewBuilder()
	d := stream.NewDecoder(r)
	for {
		ev, err := d.Next()
		if err != nil {
			break
		}
		b.Add(ev)
	}
	return b.Finish(), d.Stats(), d.Preamble()
}

func wrap(rest []string, cfg config.Config, errOut io.Writer) int {
	argv := goTestArgv(rest)

	var run *model.Run
	var stats stream.Stats
	var preamble string
	res, err := runnerpkg.Run(argv, func(r io.Reader) error {
		run, stats, preamble = consume(r)
		return nil
	})
	if err != nil {
		fmt.Fprintf(errOut, "[qualflare-go] could not run %s: %v\n", strings.Join(argv, " "), err)
		return 1
	}

	// stderr carries the compiler error when the stream does not.
	if preamble == "" {
		preamble = res.Stderr
	}
	exit := res.ExitCode
	if code := write(run, cfg, stats, preamble, &exit, errOut); code != 0 {
		return code
	}
	// The reporter is not the gate: `go test`'s own verdict is passed through
	// unchanged, so this is a drop-in replacement for it.
	return res.ExitCode
}

// goTestArgv builds the command to run. `qualflare-go ./...` is shorthand for
// `qualflare-go -- go test ./...`, and -json is added either way so nobody has
// to remember it.
func goTestArgv(rest []string) []string {
	argv := rest
	if argv[0] != "go" {
		argv = append([]string{"go", "test"}, argv...)
	}
	for _, a := range argv {
		if a == "-json" || a == "--json" {
			return argv
		}
	}
	out := append([]string{}, argv[:2]...)
	return append(append(out, "-json"), argv[2:]...)
}

func fromPipe(stdin io.Reader, cfg config.Config, errOut io.Writer) int {
	fmt.Fprintln(errOut, "[qualflare-go] reading from stdin: the exit code and stderr are not visible this way, "+
		"so a build failure on Go before 1.24 cannot be detected. Prefer: qualflare-go ./...")
	run, stats, preamble := consume(stdin)
	return write(run, cfg, stats, preamble, nil, errOut)
}

func fromFiles(paths []string, cfg config.Config, errOut io.Writer) int {
	b := model.NewBuilder()
	var stats stream.Stats
	var preamble string
	for _, p := range paths {
		fh, err := os.Open(p)
		if err != nil {
			fmt.Fprintf(errOut, "[qualflare-go] %v\n", err)
			return 1
		}
		d := stream.NewDecoder(fh)
		for {
			ev, err := d.Next()
			if err != nil {
				break
			}
			b.Add(ev)
		}
		s := d.Stats()
		stats.UnparsedLines += s.UnparsedLines
		stats.OversizedLines += s.OversizedLines
		preamble += d.Preamble()
		fh.Close()
	}
	return write(b.Finish(), cfg, stats, preamble, nil, errOut)
}

func write(run *model.Run, cfg config.Config, stats stream.Stats, preamble string, exit *int, errOut io.Writer) int {
	report := build.Collect(build.Input{
		Run: run, Cfg: cfg, Stats: stats, Preamble: preamble, ExitCode: exit,
	})

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		fmt.Fprintf(errOut, "[qualflare-go] %v\n", err)
		return 1
	}
	// Named "golang", never "go": content detection matches the
	// framework+metadata+suites triple first, but if it ever fails, the CLI's
	// filename fallback matches a word-boundary "go" token plus .json and would
	// route the file to its NDJSON go-test parser, producing garbage. With
	// "golang" in the name that fallback errors instead. Fail loud, never wrong.
	name := fmt.Sprintf("qualflare-golang-%d-%s.json", os.Getpid(), cfg.RunID)
	path := filepath.Join(cfg.OutputDir, name)

	data, err := json.Marshal(report)
	if err != nil {
		fmt.Fprintf(errOut, "[qualflare-go] %v\n", err)
		return 1
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(errOut, "[qualflare-go] %v\n", err)
		return 1
	}

	fmt.Fprintf(errOut, "[qualflare-go] wrote %d suite(s), %d case(s) to %s\n",
		len(report.Suites), countCases(report), path)
	reportAnomalies(stats, errOut)
	return 0
}

func countCases(c wire.Collect) int {
	n := 0
	for _, s := range c.Suites {
		n += len(s.Cases)
	}
	return n
}

// reportAnomalies surfaces what the decoder had to tolerate. Staying quiet
// about them would mean uploading a report that may be missing the only
// evidence of a failure.
func reportAnomalies(stats stream.Stats, errOut io.Writer) {
	if stats.UnparsedLines > 0 {
		fmt.Fprintf(errOut, "[qualflare-go] %d line(s) were not JSON and could not be attributed\n", stats.UnparsedLines)
	}
	if stats.OversizedLines > 0 {
		fmt.Fprintf(errOut, "[qualflare-go] %d line(s) exceeded the line cap and were truncated\n", stats.OversizedLines)
	}
	for action, n := range stats.UnknownActions {
		fmt.Fprintf(errOut, "[qualflare-go] ignored %d event(s) with unknown action %q -- a newer Go may be emitting something we do not read\n", n, action)
	}
}

func newRunID() string {
	return fmt.Sprintf("local-%d-%d", os.Getpid(), time.Now().UnixNano())
}
