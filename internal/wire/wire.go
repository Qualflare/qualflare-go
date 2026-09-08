// Package wire is the Qualflare Collect contract.
//
// A faithful port of the shape the seven sibling reporters emit (see
// qualflare-pytest/src/qualflare_pytest/wire.py), because qualflare-cli
// identifies this format by the TRIPLE framework + metadata + suites. Drop any
// one of those and `qf collect` falls back to filename detection and misroutes
// the file.
//
// Three rules are easy to get wrong writing this fresh, and all three are
// load-bearing:
//
//   - durations are INTEGER NANOSECONDS with no unit marker on the wire;
//   - Branch, Commit and Milestone must be PRESENT as value-or-null, never
//     omitted -- the server distinguishes "not reported" from "absent";
//   - Attempts carries EVERY attempt including the final one, 1-based, and is
//     omitted entirely below two, because fewer than two persists nothing.
//
// Go adds a fourth trap the Python original does not have: `omitempty` on a
// plain int silently drops a legitimate zero. ParentIndex 0 is the first step,
// ShardIndex 0 is a real worker, and RetryCount 0 is a real answer. Every such
// field is a pointer here, and the wire tests pin it.
package wire

import "github.com/Qualflare/qualflare-go/internal/constants"

// Parameter is a case- or step-level parameter. A masked parameter carries no
// value at all: the server treats `masked` as a display hint and does not
// redact, so dropping it at the source is the only thing that protects it.
type Parameter struct {
	Name   string  `json:"name"`
	Value  *string `json:"value,omitempty"`
	Masked bool    `json:"masked,omitempty"`
}

type Step struct {
	Name        string      `json:"name"`
	Status      string      `json:"status"`
	Duration    int64       `json:"duration"` // nanoseconds
	Keyword     string      `json:"keyword,omitempty"`
	Error       string      `json:"error,omitempty"`
	Location    string      `json:"location,omitempty"`
	ParentIndex *int        `json:"parentIndex,omitempty"` // 0 is the first step
	Parameters  []Parameter `json:"parameters,omitempty"`
}

type Attachment struct {
	Name           string `json:"name"`
	MimeType       string `json:"mimeType,omitempty"`
	Content        string `json:"content,omitempty"` // base64
	LocalImagePath string `json:"localImagePath,omitempty"`
	FileSize       *int64 `json:"fileSize,omitempty"`
	StepIndex      *int   `json:"stepIndex,omitempty"`
}

type Attempt struct {
	Attempt  int    `json:"attempt"` // 1-based; the server drops anything lower
	Status   string `json:"status"`
	Duration *int64 `json:"duration,omitempty"` // nanoseconds
	Message  string `json:"message,omitempty"`
	Trace    string `json:"trace,omitempty"`
}

type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Link struct {
	URL  string `json:"url"`
	Type string `json:"type"` // issue | tms | custom
	Name string `json:"name,omitempty"`
}

type Case struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Duration    int64             `json:"duration"` // nanoseconds
	ClassName   string            `json:"className,omitempty"`
	Error       string            `json:"error,omitempty"`
	Description string            `json:"description,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	RetryCount  *int              `json:"retryCount,omitempty"`
	IsFlaky     *bool             `json:"isFlaky,omitempty"`
	ShardIndex  *int              `json:"shardIndex,omitempty"`
	StartedAt   string            `json:"startedAt,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      []Label           `json:"labels,omitempty"`
	Links       []Link            `json:"links,omitempty"`
	Steps       []Step            `json:"steps,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	// Attempts must be set through TrimAttempts, which owns the below-two and
	// cap rules; omitempty alone would happily send an array of one.
	Attempts []Attempt `json:"attempts,omitempty"`
}

type Suite struct {
	Name     string `json:"name"`
	Duration int64  `json:"duration"` // nanoseconds
	Category string `json:"category"`
	// Cases is always present, even when empty: the server validates it as
	// required, and a nil slice marshals to null and 400s the whole upload.
	Cases      []Case            `json:"cases"`
	Properties map[string]string `json:"properties,omitempty"`
}

type Metadata struct {
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	CLIName   string `json:"cliName"`
	RunID     string `json:"runId,omitempty"`
}

type Collect struct {
	Framework   string   `json:"framework"`
	Platform    string   `json:"platform"`
	OS          string   `json:"os"`
	Browser     string   `json:"browser"`
	Environment string   `json:"environment"`
	Language    string   `json:"language"`
	Metadata    Metadata `json:"metadata"`
	Suites      []Suite  `json:"suites"`
	// Value-or-null, ALWAYS present. No omitempty: the server treats an absent
	// key differently from an explicit null.
	Branch     *string           `json:"branch"`
	Commit     *string           `json:"commit"`
	Milestone  *int              `json:"milestone"`
	Properties map[string]string `json:"properties,omitempty"`

	CIProvider    string `json:"ciProvider,omitempty"`
	CIBuildNumber string `json:"ciBuildNumber,omitempty"`
	CIRunURL      string `json:"ciRunUrl,omitempty"`
	CIPRNumber    *int   `json:"ciPrNumber,omitempty"`
}

// TrimAttempts applies the two contract rules that govern attempts, in the one
// place that owns them.
//
// Below two entries it returns nil: the server persists nothing for a lone
// attempt, so sending one is body bytes spent on a row that is discarded.
//
// Above MaxAttemptsPerCase it keeps the head and the FINAL attempt. A plain
// head-slice would discard the only attempt that explains the outcome, and the
// server applies the same head+final rule, so ours and its agree rather than
// compounding.
func TrimAttempts(attempts []Attempt) []Attempt {
	if len(attempts) < 2 {
		return nil
	}
	if len(attempts) <= constants.MaxAttemptsPerCase {
		return attempts
	}
	out := make([]Attempt, 0, constants.MaxAttemptsPerCase)
	out = append(out, attempts[:constants.MaxAttemptsPerCase-1]...)
	return append(out, attempts[len(attempts)-1])
}

// NewSuite returns a Suite whose Cases is non-nil, which is the only safe way
// to construct one -- see the Cases field comment.
func NewSuite(name, category string) Suite {
	return Suite{Name: name, Category: category, Cases: []Case{}}
}

func IntPtr(v int) *int          { return &v }
func Int64Ptr(v int64) *int64    { return &v }
func BoolPtr(v bool) *bool       { return &v }
func StringPtr(v string) *string { return &v }
