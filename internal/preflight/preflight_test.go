package preflight

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestParseRelease(t *testing.T) {
	tests := []struct {
		release string
		want    Version
		wantErr bool
	}{
		{release: "6.6.0", want: Version{6, 6}},
		{release: "6.12.0-1-amd64", want: Version{6, 12}},
		{release: "6.18.44-r0-gcp-6.18", want: Version{6, 18}},
		{release: "5.15.0-1051-azure", want: Version{5, 15}},
		{release: "6.6-rc1", want: Version{6, 6}},
		{release: "6.1.0-rpi7-rpi-v8", want: Version{6, 1}},
		{release: "4.19", want: Version{4, 19}},
		{release: "", wantErr: true},
		{release: "6", wantErr: true},
		{release: "linux", wantErr: true},
		{release: "x.6.0", wantErr: true},
		{release: "6.x.0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.release, func(t *testing.T) {
			got, err := ParseRelease(tt.release)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestVersionBefore(t *testing.T) {
	tests := []struct {
		v, o Version
		want bool
	}{
		{Version{6, 5}, MinKernel, true},
		{Version{5, 19}, MinKernel, true},
		{Version{6, 6}, MinKernel, false},
		{Version{6, 12}, MinKernel, false},
		{Version{7, 0}, MinKernel, false},
	}

	for _, tt := range tests {
		require.Equal(t, tt.want, tt.v.Before(tt.o), "%s before %s", tt.v, tt.o)
	}
}

func TestMissingCapabilities(t *testing.T) {
	low := func(caps ...int) (m uint32) {
		for _, c := range caps {
			m |= 1 << uint(c)
		}
		return m
	}
	high := func(caps ...int) (m uint32) {
		for _, c := range caps {
			m |= 1 << uint(c-32)
		}
		return m
	}

	tests := []struct {
		name    string
		effLow  uint32
		effHigh uint32
		want    []string
	}{
		{
			name: "no capabilities",
			want: []string{"CAP_BPF", "CAP_PERFMON"},
		},
		{
			name:   "sys_admin alone is sufficient",
			effLow: low(unix.CAP_SYS_ADMIN),
		},
		{
			name:    "bpf and perfmon are sufficient",
			effHigh: high(unix.CAP_BPF, unix.CAP_PERFMON),
		},
		{
			name:    "bpf without perfmon",
			effHigh: high(unix.CAP_BPF),
			want:    []string{"CAP_PERFMON"},
		},
		{
			name:    "perfmon without bpf",
			effHigh: high(unix.CAP_PERFMON),
			want:    []string{"CAP_BPF"},
		},
		{
			name:   "unrelated low capabilities do not count",
			effLow: low(unix.CAP_NET_ADMIN, unix.CAP_SYS_PTRACE),
			want:   []string{"CAP_BPF", "CAP_PERFMON"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, missingCapabilities(tt.effLow, tt.effHigh))
		})
	}
}

func TestRun(t *testing.T) {
	failKernel := func() (Version, error) { return Version{5, 4}, ErrKernelTooOld }
	okKernel := func() (Version, error) { return Version{6, 12}, nil }
	failCaps := func() error { return ErrMissingCapabilities }
	okCaps := func() error { return nil }

	tests := []struct {
		name    string
		opts    []Option
		kernel  func() (Version, error)
		caps    func() error
		wantErr error
	}{
		{
			name:   "all checks pass",
			kernel: okKernel,
			caps:   okCaps,
		},
		{
			name:    "kernel failure is returned first",
			kernel:  failKernel,
			caps:    failCaps,
			wantErr: ErrKernelTooOld,
		},
		{
			name:    "capability failure",
			kernel:  okKernel,
			caps:    failCaps,
			wantErr: ErrMissingCapabilities,
		},
		{
			name:   "skip bypasses failing checks",
			opts:   []Option{WithSkip(true)},
			kernel: failKernel,
			caps:   failCaps,
		},
		{
			name:   "userspace bpf implies skip",
			opts:   []Option{WithUserspaceBPF(true)},
			kernel: failKernel,
			caps:   failCaps,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := append(tt.opts, func(o *Options) {
				o.checkKernel = tt.kernel
				o.checkCapabilities = tt.caps
			})

			err := Run(opts...)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.True(t, errors.Is(err, tt.wantErr), "got %v", err)
		})
	}
}

// TestCheckKernelReadsRunningRelease only asserts the syscall path works and
// yields a parseable version; the box this runs on may legitimately be old.
func TestCheckKernelReadsRunningRelease(t *testing.T) {
	v, err := CheckKernel()
	if err != nil {
		require.True(t, errors.Is(err, ErrKernelTooOld), "got %v", err)
	}
	require.NotZero(t, v.Major)
}

// TestCheckCapabilitiesUnprivileged asserts the capget path works whatever
// the caller's privileges: either nothing is missing or the sentinel is
// returned with a message naming the missing capabilities.
func TestCheckCapabilitiesUnprivileged(t *testing.T) {
	err := CheckCapabilities()
	if err == nil {
		return
	}
	require.True(t, errors.Is(err, ErrMissingCapabilities), "got %v", err)
	require.Contains(t, err.Error(), "CAP_")
	require.Contains(t, err.Error(), "setcap")
}

func TestCheckRelease(t *testing.T) {
	v, err := checkRelease("5.15.0-1051-azure")
	require.ErrorIs(t, err, ErrKernelTooOld)
	require.Contains(t, err.Error(), "--"+SkipFlag)
	require.Contains(t, err.Error(), MinKernel.String())
	require.Equal(t, Version{Major: 5, Minor: 15}, v)

	v, err = checkRelease("6.6.0")
	require.NoError(t, err)
	require.Equal(t, Version{Major: 6, Minor: 6}, v)

	_, err = checkRelease("garbage")
	require.Error(t, err)
}
