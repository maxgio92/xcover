package stop

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd/options"
)

func withTempPidFile(t *testing.T) string {
	t.Helper()

	orig := settings.PidFile
	settings.PidFile = filepath.Join(t.TempDir(), "test.pid")
	t.Cleanup(func() { settings.PidFile = orig })

	return settings.PidFile
}

func TestRun_MissingPIDFile(t *testing.T) {
	withTempPidFile(t)

	o := &Options{options.NewOptions()}

	err := o.Run(nil, nil)
	if !errors.Is(err, ErrNotRunningOrNotFound) {
		t.Fatalf("Run() error = %v, want ErrNotRunningOrNotFound", err)
	}
}

func TestRun_MalformedPIDFile(t *testing.T) {
	path := withTempPidFile(t)

	if err := os.WriteFile(path, []byte("not-a-pid"), 0644); err != nil {
		t.Fatalf("failed to write malformed PID file: %v", err)
	}

	o := &Options{options.NewOptions()}

	err := o.Run(nil, nil)
	if !errors.Is(err, ErrInvalidPIDFile) {
		t.Fatalf("Run() error = %v, want ErrInvalidPIDFile", err)
	}
}
