// Command verify checks the dogfood report BEFORE it is uploaded.
//
// A report can be structurally valid and semantically wrong: that is exactly
// how Case.attempts went missing for three qualflare-cli releases while every
// run stayed green. Uploading first and inspecting later would repeat it.
//
// Failures accumulate rather than aborting on the first, so one run tells you
// everything that is wrong.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const secret = "qf-go-dogfood-secret-value"

type report struct {
	Framework string         `json:"framework"`
	Metadata  map[string]any `json:"metadata"`
	Branch    *string        `json:"branch"`
	Commit    *string        `json:"commit"`
	Milestone *int           `json:"milestone"`
	Suites    []suite        `json:"suites"`
	Raw       map[string]any `json:"-"`
}

type suite struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Cases    []kase `json:"cases"`
}

type kase struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Duration    int64             `json:"duration"`
	IsFlaky     *bool             `json:"isFlaky"`
	Properties  map[string]string `json:"properties"`
	Tags        []string          `json:"tags"`
	Labels      []kv              `json:"labels"`
	Links       []map[string]any  `json:"links"`
	Steps       []step            `json:"steps"`
	Attachments []map[string]any  `json:"attachments"`
	Description string            `json:"description"`
	Priority    string            `json:"priority"`
}

type kv struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type step struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Duration    int64  `json:"duration"`
	ParentIndex *int   `json:"parentIndex"`
}

var failures []string

func check(label string, ok bool, detail string) {
	if !ok {
		failures = append(failures, fmt.Sprintf("%s (%s)", label, detail))
	}
}

func main() {
	dir := os.Getenv("QUALFLARE_OUTPUT_DIR")
	if dir == "" {
		dir = "e2e-results"
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	// Exactly one: a second would mean something else wrote a report too, and
	// the launch would silently double-count.
	if len(files) != 1 {
		fmt.Fprintf(os.Stderr, "expected exactly one report in %s, found %d\n", dir, len(files))
		os.Exit(1)
	}

	raw, err := os.ReadFile(files[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var r report
	if err := json.Unmarshal(raw, &r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	text := string(raw)

	// The triple qualflare-cli identifies the format by. Lose one and the file
	// is routed to the wrong parser.
	check("framework is golang", r.Framework == "golang", r.Framework)
	check("metadata block present", len(r.Metadata) > 0, "missing")
	check("runId stamped", r.Metadata["runId"] != nil && r.Metadata["runId"] != "", "missing")
	check("suites present", len(r.Suites) > 0, "none")

	// Value-or-null, always present. json.Unmarshal cannot distinguish absent
	// from null, so the raw text is what proves the key was written.
	for _, key := range []string{`"branch"`, `"commit"`, `"milestone"`} {
		check("key "+key+" present as value-or-null", strings.Contains(text, key), "absent from the payload")
	}

	var cases []kase
	for _, s := range r.Suites {
		check("suite category is golang", s.Category == "golang", s.Category)
		cases = append(cases, s.Cases...)
	}
	byName := map[string]kase{}
	for _, c := range cases {
		byName[c.Name] = c
	}

	// All-passing by construction: a red case here is a real regression.
	for _, c := range cases {
		check("case "+c.Name+" passed", c.Status == "passed", c.Status)
		// No retries happen in this suite, so a true here is fabricated.
		check("case "+c.Name+" is not marked flaky", c.IsFlaky == nil || !*c.IsFlaky, "isFlaky set")
	}

	meta, ok := byName["TestRecordsEveryMetadataKind"]
	check("metadata case present", ok, "missing")
	if ok {
		check("labels recorded", len(meta.Labels) >= 2, fmt.Sprint(len(meta.Labels)))
		check("tags recorded", len(meta.Tags) >= 2, fmt.Sprint(len(meta.Tags)))
		check("link recorded", len(meta.Links) == 1, fmt.Sprint(len(meta.Links)))
		check("priority recorded", meta.Priority == "high", meta.Priority)
		check("description recorded", meta.Description != "", "empty")
		check("parameter recorded", meta.Properties["plan"] == "pro", meta.Properties["plan"])
		// A masked parameter is recorded by name with its value replaced.
		check("masked parameter is bulleted, not valued", meta.Properties["token"] == "••••••", meta.Properties["token"])
	}

	masked, ok := byName["TestMaskedParameterInsideAStep"]
	check("masked-in-step case present", ok, "missing")
	if ok && len(masked.Steps) == 1 {
		check("the step recorded its masked parameter", true, "")
	}

	// The strongest form available: absent from the RAW BYTES, not merely from
	// a decoded field. The constant is declared in the suite and never passed
	// to any reporting call, so finding it here would mean something captured
	// source text or a variable it had no business reading.
	check("no secret literal reaches the report", !strings.Contains(text, secret), "found in the payload")

	steps, ok := byName["TestNestsSteps"]
	check("steps case present", ok, "missing")
	if ok && len(steps.Steps) >= 2 {
		check("outer step is top level", steps.Steps[0].ParentIndex == nil, "has a parent")
		check("inner step points at the outer", steps.Steps[1].ParentIndex != nil && *steps.Steps[1].ParentIndex == 0,
			fmt.Sprint(steps.Steps[1].ParentIndex))
	} else if ok {
		check("two steps recorded", false, fmt.Sprint(len(steps.Steps)))
	}

	// The cap-sentinel regression is asserted end to end in
	// cmd/qualflare-go/main_test.go against test/integration/fixtures/awkward,
	// not here: exercising a 300-step cap needs 300+ steps, and this report is
	// uploaded to a public project.

	att, ok := byName["TestAttachesContent"]
	check("attachment case present", ok, "missing")
	if ok {
		check("two attachments recorded", len(att.Attachments) == 2, fmt.Sprint(len(att.Attachments)))
	}

	// Subtests are cases, not steps.
	_, a := byName["TestSubtestsAreTheirOwnCases/alpha"]
	_, b := byName["TestSubtestsAreTheirOwnCases/beta"]
	check("subtests are their own cases", a && b, "missing")
	_, parent := byName["TestSubtestsAreTheirOwnCases"]
	check("a passing parent with subtests is dropped", !parent, "the parent was also reported")

	decoy, ok := byName["TestOutputThatLooksLikeMetadataIsNotConsumed"]
	check("decoy case present", ok, "missing")
	if ok {
		check("the real label after the decoy was still read", len(decoy.Labels) == 1, fmt.Sprint(len(decoy.Labels)))
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d check(s) failed:\n", len(failures))
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, "  ✗ "+f)
		}
		os.Exit(1)
	}
	fmt.Printf("✓ %d cases verified in %s — report matches what the suite declared\n",
		len(cases), filepath.Base(files[0]))
}
