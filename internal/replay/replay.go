// Package replay turns a test's sentinel messages into the fields of one Case.
//
// Messages arrive in emission order, and that order is the only thing that
// makes steps reconstructable: there is no step id on the wire. A step_start
// pushes onto a stack, a step_stop pops, and a parameter emitted while a step is
// open belongs to that step rather than to the case.
//
// A direct port of qualflare-pytest's replay.py, including the sentinel
// described at the cap below -- which exists because of a measured bug, not a
// hypothetical one.
package replay

import (
	"fmt"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/wire"
)

// Replayed is everything the messages contributed to one case.
type Replayed struct {
	Labels         []wire.Label
	Links          []wire.Link
	Tags           []string
	Steps          []wire.Step
	Attachments    []wire.Attachment
	CaseParameters []wire.Parameter
	Description    string
	Priority       string
	Warnings       []string
}

var priorities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}

// Replay folds messages onto a single case's fields.
func Replay(msgs []sentinel.Message) Replayed {
	out := Replayed{}
	// openSteps holds indices into out.Steps. A negative entry is the cap
	// sentinel described below.
	var openSteps []int
	droppedSteps := false

	for _, m := range msgs {
		switch m.Kind {
		case sentinel.KindLabel:
			if len(out.Labels) < constants.MaxLabelsPerCase {
				out.Labels = append(out.Labels, wire.Label{Name: m.Name, Value: m.Value})
			}

		case sentinel.KindLink:
			if len(out.Links) < constants.MaxLinksPerCase {
				linkType := m.Type
				if linkType == "" {
					linkType = "custom"
				}
				out.Links = append(out.Links, wire.Link{URL: m.URL, Type: linkType, Name: m.Name})
			}

		case sentinel.KindTag:
			for _, tag := range m.Tags {
				if len(out.Tags) >= constants.MaxTagsPerCase {
					break
				}
				out.Tags = append(out.Tags, tag)
			}

		case sentinel.KindDescription:
			out.Description = m.Text

		case sentinel.KindPriority:
			// An unrecognised value is dropped rather than sent: the server
			// normalises it away anyway, and a bad value is not worth a row.
			if priorities[m.Value] {
				out.Priority = m.Value
			}

		case sentinel.KindParameter:
			param := wire.Parameter{Name: m.Name, Masked: m.Masked}
			if !m.Masked {
				v := m.Value
				param.Value = &v
			}
			if target := innermost(openSteps); target >= 0 {
				step := &out.Steps[target]
				if len(step.Parameters) < constants.MaxParametersPerStep {
					step.Parameters = append(step.Parameters, param)
				}
			} else {
				out.CaseParameters = append(out.CaseParameters, param)
			}

		case sentinel.KindStepStart:
			if len(out.Steps) >= constants.MaxStepsPerTestAttempt {
				droppedSteps = true
				// A SENTINEL, not a bare skip. The dropped step's matching stop
				// still arrives, and with nothing to pop it would close whichever
				// step is legitimately open -- overwriting that step's status and
				// duration, and then discarding its real stop because the stack
				// is empty. Measured in the pytest reporter: an outer step
				// wrapping a 50ms sleep reported 0.000ms once the cap was
				// crossed.
				openSteps = append(openSteps, -1)
				continue
			}
			step := wire.Step{Name: m.Name, Status: "passed"}
			if parent := innermost(openSteps); parent >= 0 {
				step.ParentIndex = wire.IntPtr(parent)
			}
			out.Steps = append(out.Steps, step)
			openSteps = append(openSteps, len(out.Steps)-1)

		case sentinel.KindStepStop:
			if len(openSteps) == 0 {
				continue
			}
			index := openSteps[len(openSteps)-1]
			openSteps = openSteps[:len(openSteps)-1]
			if index < 0 {
				continue // the cap sentinel: consumed and ignored
			}
			step := &out.Steps[index]
			if m.Status != "" {
				step.Status = m.Status
			}
			step.Duration = m.Duration
			if m.Error != "" {
				step.Error = m.Error
			}

		case sentinel.KindAttachment:
			if len(out.Attachments) < constants.MaxAttachmentsPerCase {
				if len(m.Content) > constants.MaxAttachmentInlineChars {
					// Dropped rather than truncated: half a base64 payload is not
					// a usable file, and an oversized body is rejected whole,
					// losing the entire launch rather than this one attachment.
					out.Warnings = append(out.Warnings,
						fmt.Sprintf("skipping attachment %q: %d encoded bytes exceeds the inline cap", m.Name, len(m.Content)))
					out.Attachments = append(out.Attachments, wire.Attachment{Name: m.Name, MimeType: m.MimeType})
					continue
				}
				out.Attachments = append(out.Attachments, wire.Attachment{
					Name: m.Name, MimeType: m.MimeType, Content: m.Content,
				})
			}

		case sentinel.KindWarning:
			out.Warnings = append(out.Warnings, m.Text)
		}
	}

	if droppedSteps {
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("a test produced more than %d steps; the rest were dropped", constants.MaxStepsPerTestAttempt))
	}
	return out
}

// innermost returns the index of the innermost step that is really open,
// skipping cap sentinels. Returns -1 when nothing is open.
func innermost(open []int) int {
	for i := len(open) - 1; i >= 0; i-- {
		if open[i] >= 0 {
			return open[i]
		}
	}
	return -1
}
