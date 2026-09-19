package trace

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stretchr/testify/require"

	"github.com/maxgio92/xcover/pkg/probe"
)

// countingProbe records Init calls without loading a BPF object. onInit, when
// set, runs inside Init so a test can observe tracer state at that moment.
type countingProbe struct {
	initCalls int
	onInit    func()
}

func (f *countingProbe) Init(context.Context) error {
	f.initCalls++
	if f.onInit != nil {
		f.onInit()
	}
	return nil
}
func (f *countingProbe) DetachLinks() {}
func (f *countingProbe) Attach(context.Context, string, []uint64, []uint64) error {
	return nil
}
func (f *countingProbe) InitEventBuf(context.Context) (chan []byte, error) { return nil, nil }
func (f *countingProbe) PollEventBuf()                                     {}
func (f *countingProbe) CloseEventBuf()                                    {}
func (f *countingProbe) CloseBPFMod()                                      {}
func (f *countingProbe) Drops() (uint64, error)                            { return 0, nil }
func (f *countingProbe) CheckPIDFilter() error                             { return nil }

// TestUserTracerInit_ProbeSizedAfterTracee asserts that Init resolves the
// tracee functions before initializing the probe, so the seen_funcs map can be
// sized before the BPF object is loaded; TestDefaultProbe_CarriesFuncCount
// covers the count itself.
func TestUserTracerInit_ProbeSizedAfterTracee(t *testing.T) {
	// Keep the health check socket out of the shared /tmp path.
	origSock := HealthCheckSockPath
	HealthCheckSockPath = filepath.Join(t.TempDir(), "hc.sock")
	t.Cleanup(func() { HealthCheckSockPath = origSock })

	entries := []FunctionEntry{
		{Name: "pkg.Alpha", Offset: 0x1000},
		{Name: "pkg.Beta", Offset: 0x2000},
		{Name: "pkg.Gamma", Offset: 0x3000},
	}
	tracee := NewUserTracee(
		WithTraceeExePath("dummy-path"),
		WithTraceeResolver(func(context.Context) ([]FunctionEntry, error) { return entries, nil }),
		WithTraceeLogger(zerolog.Nop()),
	)

	tracer := NewUserTracer(
		WithTracerLogger(zerolog.Nop()),
		WithTracerTracee(tracee),
	)

	var funcsAtInit int
	probe := &countingProbe{onInit: func() { funcsAtInit = len(tracee.funcs) }}
	tracer.probe = probe

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, tracer.Init(ctx))
	t.Cleanup(func() { require.NoError(t, tracer.hcServer.ShutdownListener()) })

	require.Equal(t, len(entries), funcsAtInit, "tracee.Init must run before the probe is initialized")
	require.Equal(t, 1, probe.initCalls)
}

// TestUserTracerInit_InjectedProbe asserts that a probe injected with
// WithTracerProbe is initialized as-is and no default probe is built.
func TestUserTracerInit_InjectedProbe(t *testing.T) {
	origSock := HealthCheckSockPath
	HealthCheckSockPath = filepath.Join(t.TempDir(), "hc.sock")
	t.Cleanup(func() { HealthCheckSockPath = origSock })

	tracee := NewUserTracee(
		WithTraceeExePath("dummy-path"),
		WithTraceeResolver(func(context.Context) ([]FunctionEntry, error) {
			return []FunctionEntry{{Name: "pkg.Alpha", Offset: 0x1000}}, nil
		}),
		WithTraceeLogger(zerolog.Nop()),
	)
	probe := &countingProbe{}
	tracer := NewUserTracer(
		WithTracerLogger(zerolog.Nop()),
		WithTracerTracee(tracee),
		WithTracerProbe(probe),
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, tracer.Init(ctx))
	t.Cleanup(func() { require.NoError(t, tracer.hcServer.ShutdownListener()) })

	require.Equal(t, 1, probe.initCalls)
}

// TestDefaultProbe_CarriesFuncCount asserts the real probe is built with the
// tracee function count so it can size the seen_funcs map before load.
func TestDefaultProbe_CarriesFuncCount(t *testing.T) {
	tracer := NewUserTracer(WithTracerLogger(zerolog.Nop()))
	p, ok := tracer.defaultProbe(3).(*probe.Probe)
	require.True(t, ok)
	require.Equal(t, 3, p.FuncCount())
}

// TestWarnDrops asserts the drop counter surfaces as a Warn only when calls
// were dropped, since the report undercounts in that case.
func TestWarnDrops(t *testing.T) {
	for _, tt := range []struct {
		name  string
		drops uint64
		warn  bool
	}{
		{name: "no drops stays silent", drops: 0, warn: false},
		{name: "drops warn about undercounting", drops: 3, warn: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			tracer := NewUserTracer(
				WithTracerLogger(zerolog.New(&out)),
				WithTracerProbe(&fakeProbe{drops: tt.drops}),
			)
			tracer.warnDrops()
			if tt.warn {
				require.Contains(t, out.String(), `"level":"warn"`)
				require.Contains(t, out.String(), `"dropped":3`)
				require.Contains(t, out.String(), "undercounts")
			} else {
				require.Empty(t, out.String())
			}
		})
	}
}

// TestCheckPIDFilter asserts the kernel PID filter check runs only for an
// active filter in kernel mode, warns about undercounting when the kernel
// filters by thread or the check was inconclusive, and that the warning
// repeats when warnPIDFilter runs again next to the report.
func TestCheckPIDFilter(t *testing.T) {
	for _, tt := range []struct {
		name         string
		pid          int
		userspaceBPF bool
		probeErr     error
		wantCalls    int
		wantWarn     string
	}{
		{
			name:     "no filter skips the check",
			pid:      -1,
			probeErr: probe.ErrPIDFilterByThread,
		},
		{
			name:         "userspace BPF skips the check",
			pid:          42,
			userspaceBPF: true,
			probeErr:     probe.ErrPIDFilterByThread,
		},
		{
			name:      "thread group filter stays silent",
			pid:       42,
			wantCalls: 1,
		},
		{
			name:      "thread filter warns about undercounting",
			pid:       42,
			probeErr:  probe.ErrPIDFilterByThread,
			wantCalls: 1,
			wantWarn:  "filters uprobe_multi by thread instead of thread group",
		},
		{
			name:      "inconclusive check warns about undercounting",
			pid:       42,
			probeErr:  errors.New("link_create failed: operation not permitted"),
			wantCalls: 1,
			wantWarn:  "could not check whether the kernel filters uprobe_multi by thread group",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			p := &fakeProbe{pidFilterErr: tt.probeErr}
			tracer := NewUserTracer(
				WithTracerLogger(zerolog.New(&out)),
				WithTracerProbe(p),
				WithTracerPID(tt.pid),
				WithTracerUserspaceBPF(tt.userspaceBPF),
			)

			tracer.checkPIDFilter()
			require.Equal(t, tt.wantCalls, p.pidFilterCalls)
			if tt.wantWarn == "" {
				require.Empty(t, out.String())
				return
			}
			assertPIDFilterWarning := func() {
				t.Helper()
				require.Contains(t, out.String(), `"level":"warn"`)
				require.Contains(t, out.String(), `"pid":42`)
				require.Contains(t, out.String(), `"kernel":"`)
				require.Contains(t, out.String(), "undercount")
				require.Contains(t, out.String(), "drop --pid")
				require.Contains(t, out.String(), tt.wantWarn)
			}
			assertPIDFilterWarning()

			// The recorded outcome repeats at report time without another probe call.
			out.Reset()
			tracer.warnPIDFilter()
			require.Equal(t, tt.wantCalls, p.pidFilterCalls)
			assertPIDFilterWarning()
		})
	}
}
