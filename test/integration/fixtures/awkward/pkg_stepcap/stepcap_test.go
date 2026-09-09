// A test that blows past the per-attempt step cap from inside an outer step.
//
// This lives here rather than in e2e/ for a reason: exercising the cap needs
// more than 300 real steps, and e2e/ is uploaded to a public Qualflare project
// that backs the README banner. Three hundred steps named "filler" is noise in
// a report people actually read. The fixtures module is never uploaded, so the
// same coverage costs nothing.
package pkg_stepcap

import (
	"testing"
	"time"

	"github.com/Qualflare/qualflare-go"
)

// The regression the cap sentinel exists for. Without it, the dropped steps'
// stop messages close the OUTER step, so its real duration is discarded --
// measured once in the pytest reporter as a 50ms sleep reporting 0.000ms.
func TestOuterStepSurvivesTheStepCap(t *testing.T) {
	qualflare.Step(t, "wraps-a-measured-sleep", func() {
		for i := 0; i < 320; i++ {
			qualflare.Step(t, "filler", func() {})
		}
		time.Sleep(50 * time.Millisecond)
	})
}
