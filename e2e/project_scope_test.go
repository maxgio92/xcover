//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/coverage"
	"github.com/maxgio92/xcover/pkg/trace"
)

const (
	xcoverBinEnv           = "XCOVER_E2E_BIN"
	reportFile             = "xcover-report.json"
	projectScopeGoScenario = "project-scope-go-module"
	projectScope           = "project"
	binaryScope            = "binary"
)

var (
	pidFile    = settings.PidFile
	socketFile = trace.HealthCheckSockPath
	logFile    = settings.LogFile
)

func TestProjectScopeFiltersToGoModule(t *testing.T) {
	report := runXcoverWithFixture(t, projectScope)
	traced := stringSet(report.FuncsTraced)

	for _, name := range []string{
		"main.appLogic",
		"main.main",
		"example.com/testmod/pkg.Helper",
		"example.com/testmod/pkg.internal",
	} {
		if !traced[name] {
			t.Fatalf("expected project scoped report to trace %q; traced=%v", name, report.FuncsTraced)
		}
	}

	for _, name := range report.FuncsTraced {
		if strings.HasPrefix(name, "runtime.") || strings.HasPrefix(name, "fmt.") {
			t.Fatalf("project scoped report leaked non-project function %q; traced=%v", name, report.FuncsTraced)
		}
	}
}

func TestBinaryScopeRetainsNonProjectSymbols(t *testing.T) {
	report := runXcoverWithFixture(t, binaryScope)
	traced := stringSet(report.FuncsTraced)

	for _, name := range []string{
		"main.appLogic",
		"main.main",
		"example.com/testmod/pkg.Helper",
		"example.com/testmod/pkg.internal",
	} {
		if !traced[name] {
			t.Fatalf("expected binary scoped report to trace project function %q; traced=%v", name, report.FuncsTraced)
		}
	}

	if !hasAnyPrefix(report.FuncsTraced, "runtime.") {
		t.Fatalf("expected binary scoped report to retain runtime symbols; traced=%v", report.FuncsTraced)
	}
	if !hasAnyPrefix(report.FuncsTraced, "fmt.") {
		t.Fatalf("expected binary scoped report to retain fmt symbols; traced=%v", report.FuncsTraced)
	}
}

func runXcoverWithFixture(t *testing.T, scope string) coverage.CoverageReport {
	t.Helper()

	xcover := os.Getenv(xcoverBinEnv)
	if xcover == "" {
		t.Skipf("%s is not set", xcoverBinEnv)
	}
	if _, err := os.Stat(xcover); err != nil {
		t.Fatalf("xcover binary is not available at %q: %v", xcover, err)
	}
	t.Logf("using xcover binary: %s", xcover)
	requireNoRunningXcover(t)

	workDir := t.TempDir()
	bin := buildGoProjectFixture(t, workDir)
	t.Logf("built Go fixture binary: %s", bin)
	logOffset := fileSize(t, logFile)

	t.Logf("starting xcover daemon with --scope=%s", scope)
	if out, err := commandOutput(workDir, 10*time.Second, xcover,
		"--log-level=debug",
		"run",
		"--path", bin,
		"--scope="+scope,
		"--detach",
		"--status=false",
	); err != nil {
		if shouldSkipForRuntimeEnvironment(out) {
			t.Skipf("xcover e2e runtime requirements are unavailable:\n%s", strings.TrimSpace(out))
		}
		t.Fatalf("%s failed: %v\n%s", commandLine(xcover, "--log-level=debug", "run", "--path", bin, "--scope="+scope, "--detach", "--status=false"), err, out)
	}
	t.Log("xcover daemon start command returned")

	stopped := false
	defer func() {
		if !stopped {
			_, _ = commandOutput(workDir, 10*time.Second, xcover, "stop")
		}
	}()

	t.Log("waiting for xcover readiness")
	if out, err := commandOutput(workDir, 20*time.Second, xcover, "wait", "--timeout=15s"); err != nil {
		logTail := readSince(t, logFile, logOffset)
		if shouldSkipForRuntimeEnvironment(out + "\n" + logTail) {
			t.Skipf("xcover e2e runtime requirements are unavailable:\n%s", strings.TrimSpace(out+"\n"+logTail))
		}
		t.Fatalf("xcover wait failed: %v\n%s\n%s", err, out, logTail)
	}
	t.Log("xcover reported ready")

	t.Log("running fixture binary")
	runCommand(t, workDir, 10*time.Second, bin)
	t.Log("stopping xcover daemon")
	runCommand(t, workDir, 10*time.Second, xcover, "stop")
	stopped = true

	report := readReport(t, filepath.Join(workDir, reportFile))
	t.Logf("read report with %d traced functions and %d acknowledged functions", len(report.FuncsTraced), len(report.FuncsAck))
	return report
}

func requireNoRunningXcover(t *testing.T) {
	t.Helper()
	for _, path := range []string{pidFile, socketFile} {
		if _, err := os.Stat(path); err == nil {
			t.Skipf("skipping e2e test because %s already exists", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed to inspect %s: %v", path, err)
		}
	}

	if _, err := os.Stat(logFile); err == nil {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Skipf("skipping e2e test because %s is not writable: %v", logFile, err)
		}
		file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed to inspect %s: %v", logFile, err)
	}
}

func buildGoProjectFixture(t *testing.T, workDir string) string {
	t.Helper()

	srcDir := filepath.Join(workDir, "fixture")
	scenarioDir := fixtureScenarioDir(t, projectScopeGoScenario)
	if err := os.CopyFS(srcDir, os.DirFS(scenarioDir)); err != nil {
		t.Fatalf("failed to copy fixture scenario %s: %v", projectScopeGoScenario, err)
	}

	bin := filepath.Join(workDir, "testmod")
	runCommand(t, srcDir, 30*time.Second, "go", "build", "-buildvcs=false", "-o", bin, ".")
	return bin
}

func fixtureScenarioDir(t *testing.T, scenario string) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate e2e source file")
	}
	return filepath.Join(filepath.Dir(file), "testdata", scenario)
}

func runCommand(t *testing.T, dir string, timeout time.Duration, name string, args ...string) {
	t.Helper()
	if out, err := commandOutput(dir, timeout, name, args...); err != nil {
		t.Fatalf("%s failed: %v\n%s", commandLine(name, args...), err, out)
	}
}

func commandOutput(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("%s: %w", commandLine(name, args...), ctx.Err())
	}
	return string(out), err
}

func commandLine(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	return info.Size()
}

func readSince(t *testing.T, path string, offset int64) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	if offset >= int64(len(data)) {
		return ""
	}
	return string(data[offset:])
}

func shouldSkipForRuntimeEnvironment(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "operation not permitted") ||
		strings.Contains(output, "permission denied") ||
		strings.Contains(output, "failed to load bpf object") ||
		strings.Contains(output, "error initializing bpf probe")
}

func readReport(t *testing.T, path string) coverage.CoverageReport {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read report %s: %v", path, err)
	}

	var report coverage.CoverageReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("failed to parse report %s: %v", path, err)
	}
	return report
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func hasAnyPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
