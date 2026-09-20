//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

const demangleCppScenario = "demangle-cpp"

// TestCppReportCarriesDemangledNames traces a C++ fixture in binary scope and
// checks that the report pairs the mangled ns::compute symbol with its
// demangled name.
func TestCppReportCarriesDemangledNames(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildCppFixture(t, workDir, demangleCppScenario)
	t.Logf("built C++ fixture binary: %s", bin)

	report := runXcoverSession(t, xcover, workDir, []string{"--path", bin, "--scope=" + binaryScope}, func() {
		t.Log("running fixture binary")
		runCommand(t, workDir, 10*time.Second, bin)
	})

	for _, fn := range report.Functions {
		if strings.Contains(fn.Demangled, "ns::compute") {
			return
		}
	}
	t.Fatalf("expected a function with demangled name containing %q; functions=%v", "ns::compute", report.Functions)
}
