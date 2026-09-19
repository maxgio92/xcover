package preflight

import (
	"bytes"
	"testing"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
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

// TestIsIdentityUIDMap uses the exact content /proc/self/uid_map has on a
// host (identity) and under unshare -Ur (a single rootless line); the
// multi-line case is the shape rootless container engines produce with a
// subordinate range from /etc/subuid (user_namespaces(7)).
func TestIsIdentityUIDMap(t *testing.T) {
	tests := []struct {
		name    string
		uidMap  string
		want    bool
		wantErr bool
	}{
		{name: "identity as printed by the kernel", uidMap: "         0          0 4294967295\n", want: true},
		{name: "identity without padding", uidMap: "0 0 4294967295", want: true},
		{name: "unshare -Ur", uidMap: "         0       1000          1\n"},
		{name: "rootless container with a subordinate range", uidMap: "         0       1000          1\n         1     100000      65536\n"},
		{name: "identity range from a non-zero id", uidMap: "         0        100 4294967295\n"},
		{name: "empty map of a namespace without a mapping", uidMap: ""},
		{name: "two fields", uidMap: "0 0\n", wantErr: true},
		{name: "four fields", uidMap: "0 0 4294967295 0\n", wantErr: true},
		{name: "non-numeric field", uidMap: "0 root 4294967295\n", wantErr: true},
		{name: "length overflows uint32", uidMap: "0 0 4294967296\n", wantErr: true},
		{name: "negative id", uidMap: "-1 0 4294967295\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := isIdentityUIDMap(tt.uidMap)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRun(t *testing.T) {
	oldKernel := func() (Version, string, error) { return Version{5, 14}, "kernel 5.14 is older than 6.6", nil }
	unreadableKernel := func() (Version, string, error) { return Version{}, "", errors.New("uname failed") }
	okKernel := func() (Version, string, error) { return Version{6, 12}, "", nil }
	failCaps := func() error { return ErrMissingCapabilities }
	okCaps := func() error { return nil }
	initialNS := func() (bool, error) { return false, nil }
	childNS := func() (bool, error) { return true, nil }
	unreadableNS := func() (bool, error) { return false, errors.New("open /proc/self/uid_map: no such file or directory") }

	tests := []struct {
		name     string
		opts     []Option
		kernel   func() (Version, string, error)
		caps     func() error
		userNS   func() (bool, error) // nil means the initial namespace
		wantErr  error
		wantWarn string
	}{
		{
			name:   "all checks pass",
			kernel: okKernel,
			caps:   okCaps,
		},
		{
			name:     "old kernel is a warning, not an error",
			kernel:   oldKernel,
			caps:     okCaps,
			wantWarn: "older than 6.6",
		},
		{
			name:     "unreadable kernel is a warning, not an error",
			kernel:   unreadableKernel,
			caps:     okCaps,
			wantWarn: "kernel version not checked",
		},
		{
			name:     "old kernel does not mask the capability failure",
			kernel:   oldKernel,
			caps:     failCaps,
			wantErr:  ErrMissingCapabilities,
			wantWarn: "older than 6.6",
		},
		{
			name:    "capability failure",
			kernel:  okKernel,
			caps:    failCaps,
			wantErr: ErrMissingCapabilities,
		},
		{
			name:     "user namespace is a warning once capabilities pass",
			kernel:   okKernel,
			caps:     okCaps,
			userNS:   childNS,
			wantWarn: "running in a user namespace",
		},
		{
			name:     "unreadable uid map is a warning, not an error",
			kernel:   okKernel,
			caps:     okCaps,
			userNS:   unreadableNS,
			wantWarn: "user namespace not checked",
		},
		{
			name:    "capability failure is not followed by the namespace warning",
			kernel:  okKernel,
			caps:    failCaps,
			userNS:  childNS,
			wantErr: ErrMissingCapabilities,
		},
		{
			name:   "skip bypasses failing checks and silences the advisory",
			opts:   []Option{WithSkip(true)},
			kernel: oldKernel,
			caps:   failCaps,
			userNS: childNS,
		},
		{
			name:   "userspace bpf implies skip",
			opts:   []Option{WithUserspaceBPF(true)},
			kernel: oldKernel,
			caps:   failCaps,
			userNS: childNS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userNS := tt.userNS
			if userNS == nil {
				userNS = initialNS
			}

			var logs bytes.Buffer
			opts := append(tt.opts, WithLogger(log.New(&logs)), func(o *Options) {
				o.checkKernel = tt.kernel
				o.checkCapabilities = tt.caps
				o.inUserNamespace = userNS
			})

			err := Run(opts...)
			if tt.wantWarn == "" {
				require.NotContains(t, logs.String(), `"level":"warn"`)
			} else {
				require.Contains(t, logs.String(), `"level":"warn"`)
				require.Contains(t, logs.String(), tt.wantWarn)
			}
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
	v, advisory, err := CheckKernel()
	require.NoError(t, err)
	require.NotZero(t, v.Major)
	require.Equal(t, v.Before(MinKernel), advisory != "")
}

// TestInUserNamespaceReadsProc only asserts the /proc read and parse path
// works; whether the test runs in a namespace depends on the box.
func TestInUserNamespaceReadsProc(t *testing.T) {
	_, err := inUserNamespace()
	require.NoError(t, err)
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
	v, advisory, err := checkRelease("5.14.0-427.13.1.el9_4.x86_64")
	require.NoError(t, err)
	require.Equal(t, Version{Major: 5, Minor: 14}, v)
	require.Contains(t, advisory, "5.14.0-427.13.1.el9_4.x86_64")
	require.Contains(t, advisory, MinKernel.String())
	require.Contains(t, advisory, "backport")
	require.NotContains(t, advisory, SkipFlag)

	v, advisory, err = checkRelease("6.6.0")
	require.NoError(t, err)
	require.Equal(t, Version{Major: 6, Minor: 6}, v)
	require.Empty(t, advisory)

	_, _, err = checkRelease("garbage")
	require.Error(t, err)
}
