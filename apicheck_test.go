package qualflare_test

import (
	"os/exec"
	"strings"
	"testing"
)

// A user importing this package inherits its whole import graph into their test
// binary. These guard that graph, because it is easy to lose by accident: the
// binary half of this module legitimately needs os/exec and net-adjacent
// packages, and a single stray import from the public package would drag them
// into every consumer.
//
// This is a gate, not an aspiration -- which is why it is a test rather than a
// note in the README.

func deps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestPublicPackage_ImportsNoNetworkClient(t *testing.T) {
	// The reporter makes no network calls; qualflare-cli uploads. Proving the
	// absence of a client over the real import graph is stronger than grepping
	// for one -- a mock can lie about the network, an absent package cannot.
	forbidden := map[string]bool{
		"net/http": true, "net": true, "net/url": true, "crypto/tls": true,
	}
	for _, dep := range deps(t, ".") {
		if forbidden[dep] {
			t.Errorf("the public package must not import %q", dep)
		}
	}
}

func TestPublicPackage_DoesNotDragInTheBinarysDependencies(t *testing.T) {
	// os/exec arrives through gitdetect, which only the binary needs. If it
	// ever appears here, some internal package has been imported from the
	// public surface by mistake.
	for _, dep := range deps(t, ".") {
		if dep == "os/exec" {
			t.Error("the public package must not import os/exec -- it belongs to the binary half")
		}
	}
}

func TestPublicPackage_ImportsOnlyItsThreeInternalPackages(t *testing.T) {
	// Keeping this narrow is what stops a wire-contract change from rebuilding
	// every user's test binary.
	allowed := map[string]bool{
		"github.com/Qualflare/qualflare-go":                     true,
		"github.com/Qualflare/qualflare-go/internal/sentinel":   true,
		"github.com/Qualflare/qualflare-go/internal/constants":  true,
		"github.com/Qualflare/qualflare-go/internal/textutil":   true,
	}
	for _, dep := range deps(t, ".") {
		if !strings.HasPrefix(dep, "github.com/Qualflare/qualflare-go") {
			continue // stdlib
		}
		if !allowed[dep] {
			t.Errorf("unexpected internal import %q in the public package", dep)
		}
	}
}

func TestModule_HasNoExternalDependencies(t *testing.T) {
	// A reporter that drags a dependency into every user's go.mod is a tax.
	out, err := exec.Command("go", "list", "-m", "all").Output()
	if err != nil {
		t.Fatalf("go list -m all: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" && line != "github.com/Qualflare/qualflare-go" {
			t.Errorf("unexpected module dependency: %q", line)
		}
	}
}
