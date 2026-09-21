//go:build e2e

package e2e

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRingBufSizeSmall runs a session with --ringbuf-size=64KiB and checks
// that the report still acknowledges every executed fixture function and that
// the daemon loaded a 64 KiB buffer. 64 KiB is the smallest size the
// validator accepts on every page size Linux ships (4, 16 and 64 KiB), so the
// value is valid wherever the suite runs. It holds 4095 16-byte records, far
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

// TestRingBufSizeOnePageWarnsDrops runs a session with the events ring buffer
// sized to one page under --scope=binary, pauses the daemon while the fixture
// runs and asserts the daemon logs the drops warning with a positive dropped
// count. Each event costs 16 bytes in the ring buffer, an 8-byte record
// header plus the 8-byte event, so a 4 KiB page holds 255 records: the kernel
// rejects the reserve that would put the producer a full page ahead of the
// consumer. Under binary scope the fixture traces about 1940 functions and
// its Go runtime init hits about 560 distinct ones, each of which reserves
// one record. On an idle multi-core host the poller drains the page faster
// than that burst fills it, so the test stops the daemon with SIGSTOP for the
// fixture run:
// the uprobes still fire in the traced process, the page fills after 255
// first hits and every later bpf_ringbuf_reserve fails and is counted. SIGCONT
// then lets the daemon drain the page before stop reads the counter. The test
// does not assert on the report: main.main runs once and may itself be a
// dropped record, so no function is guaranteed to be acknowledged. It does
// not assert a specific count either: drops counts calls, a dropped first hit
// leaves the function unseen so every later call retries and fails again, and
// the total depends on the host toolchain's runtime. A 16 or 64 KiB page holds
// 1023 or 4095 records, more than the fixture hits, so the test skips there
// with a plain t.Skipf: a larger page is a valid environment, not an unmet
// precondition, and XCOVER_E2E_REQUIRE=1 must not fail it.
func TestRingBufSizeOnePageWarnsDrops(t *testing.T) {
	pageSize := os.Getpagesize()
	if pageSize > 4096 {
		t.Skipf("page size %d holds %d records, more than the fixture hits, so the ring buffer cannot fill", pageSize, pageSize/16-1)
	}

	xcover := xcoverBinary(t)
	workDir := t.TempDir()
	bin := buildGoFixture(t, workDir, projectScopeGoScenario)
	t.Logf("built Go fixture binary: %s", bin)

	logOffset := fileSize(t, logFile)
	runXcoverSession(t, xcover, workDir, []string{"--path", bin, "--scope=" + binaryScope, "--ringbuf-size=" + strconv.Itoa(pageSize)}, func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatalf("failed to read PID file %s: %v", pidFile, err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatalf("PID file %s holds %q, not a PID: %v", pidFile, data, err)
		}
		// The cleanup runs before the one startXcoverDaemon registered, so a
		// failed fixture run never leaves a stopped daemon for stop to wait on.
		// It signals only while the daemon is paused: once resumed or exited,
		// the PID may belong to another process.
		paused := false
		t.Cleanup(func() {
			if paused {
				_ = syscall.Kill(pid, syscall.SIGCONT)
			}
		})
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			t.Fatalf("failed to pause daemon %d: %v", pid, err)
		}
		paused = true
		t.Logf("paused xcover daemon %d", pid)
		runCommand(t, workDir, 10*time.Second, bin)
		if err := syscall.Kill(pid, syscall.SIGCONT); err != nil {
			t.Fatalf("failed to resume daemon %d: %v", pid, err)
		}
		paused = false
		t.Logf("resumed xcover daemon %d", pid)
	})

	plain := ansiEscape.ReplaceAllString(readSince(t, logFile, logOffset), "")
	sizeLogged := regexp.MustCompile(`events ring buffer size.*\D` + strconv.Itoa(pageSize) + `(\D|$)`)
	if !sizeLogged.MatchString(plain) {
		t.Fatalf("daemon log does not record a %d byte events ring buffer:\n%s", pageSize, plain)
	}
	if !strings.Contains(plain, dropsWarningText) {
		t.Fatalf("daemon log does not contain %q:\n%s", dropsWarningText, plain)
	}
	match := droppedField.FindStringSubmatch(plain)
	if match == nil {
		t.Fatalf("daemon log has the drops warning but no dropped=N field:\n%s", plain)
	}
	dropped, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		t.Fatalf("failed to parse dropped count %q: %v", match[1], err)
	}
	if dropped == 0 {
		t.Fatalf("expected a positive dropped count, got %d:\n%s", dropped, plain)
	}
	t.Logf("daemon reported %d dropped calls with a %d byte events ring buffer", dropped, pageSize)
}
