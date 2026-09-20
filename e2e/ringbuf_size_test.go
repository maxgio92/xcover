//go:build e2e

package e2e

import (
	"regexp"
	"testing"
	"time"
)

// TestRingBufSizeSmall runs a session with --ringbuf-size=64KiB and checks
// that the report still acknowledges every executed fixture function and that
// the daemon loaded a 64 KiB buffer. 64 KiB is the smallest size the
// validator accepts on every page size Linux ships (4, 16 and 64 KiB), so the
// value is valid wherever the suite runs. It holds 4096 16-byte records, far
// more than the handful the fixture emits, so coverage must be unchanged from
// the default buffer.
func TestRingBufSizeSmall(t *testing.T) {
	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	logOffset := fileSize(t, logFile)
	report := runXcoverSession(t, xcover, workDir, []string{"--path", bin, "--scope=" + projectScope, "--ringbuf-size=64KiB"}, func() {
		runCommand(t, workDir, 10*time.Second, bin)
	})
	assertFixtureFunctionsAcked(t, report)

	// The report alone is satisfied by the compiled 16 MiB default too. The
	// size the probe logs at init, after libbpf has had its say, is the only
	// observable proof the flag reached the kernel. The daemon logger colours
	// its output, so an ANSI reset sits between the "bytes=" key and the
	// value; \D rather than \b keeps the digits whole, since the reset ends
	// in a letter.
	sizeLogged := regexp.MustCompile(`events ring buffer size.*\D65536(\D|$)`)
	if logTail := readSince(t, logFile, logOffset); !sizeLogged.MatchString(logTail) {
		t.Fatalf("daemon log does not record a 65536 byte events ring buffer:\n%s", logTail)
	}
}
