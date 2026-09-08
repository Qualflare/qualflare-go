package pkg_parallel

import (
	"strings"
	"testing"
)

// Eight parallel tests each logging a payload far larger than test2json's 4096
// input buffer, so their output genuinely interleaves and arrives in pieces.
func TestParallelBigOutput(t *testing.T) {
	for _, name := range []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Logf("BEGIN-%s-%s-END-%s", name, strings.Repeat(name, 20000), name)
		})
	}
}
