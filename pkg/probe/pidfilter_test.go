package probe

import (
	"testing"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// TestPIDFilterVerdict pins the errno contract CheckPIDFilter shares with
// libbpf's probe_uprobe_multi_link (features.c): EINVAL is the fixed kernel
// refusing pid -1, EBADF is the broken kernel reaching the path check, and
// anything else is inconclusive.
func TestPIDFilterVerdict(t *testing.T) {
	inconclusive := func(t *testing.T, err error) {
		t.Helper()
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrPIDFilterByThread)
	}
	tests := []struct {
		name     string
		probeErr error
		check    func(t *testing.T, err error)
	}{
		{
			name:     "EINVAL means the kernel filters by thread group",
			probeErr: unix.EINVAL,
			check:    func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name:     "EBADF means the kernel filters by thread",
			probeErr: unix.EBADF,
			check:    func(t *testing.T, err error) { require.ErrorIs(t, err, ErrPIDFilterByThread) },
		},
		{
			name:     "ESRCH is inconclusive",
			probeErr: unix.ESRCH,
			check:    inconclusive,
		},
		{
			name:     "EPERM is inconclusive and kept in the error",
			probeErr: unix.EPERM,
			check: func(t *testing.T, err error) {
				inconclusive(t, err)
				require.ErrorIs(t, err, unix.EPERM)
			},
		},
		{
			name:     "an unexpected success is inconclusive",
			probeErr: nil,
			check:    inconclusive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, pidFilterVerdict(tt.probeErr))
		})
	}
}

// TestUprobeMultiLinkCreateAttrLayout pins the struct to the offsets of
// struct link_create and its uprobe_multi member in include/uapi/linux/bpf.h,
// so the raw syscall keeps matching the kernel ABI on 64-bit targets.
func TestUprobeMultiLinkCreateAttrLayout(t *testing.T) {
	var attr uprobeMultiLinkCreateAttr
	require.Equal(t, uintptr(64), unsafe.Sizeof(attr))
	require.Equal(t, uintptr(0), unsafe.Offsetof(attr.progFd))
	require.Equal(t, uintptr(4), unsafe.Offsetof(attr.targetFd))
	require.Equal(t, uintptr(8), unsafe.Offsetof(attr.attachType))
	require.Equal(t, uintptr(12), unsafe.Offsetof(attr.flags))
	require.Equal(t, uintptr(16), unsafe.Offsetof(attr.path))
	require.Equal(t, uintptr(24), unsafe.Offsetof(attr.offsets))
	require.Equal(t, uintptr(32), unsafe.Offsetof(attr.refCtrOffsets))
	require.Equal(t, uintptr(40), unsafe.Offsetof(attr.cookies))
	require.Equal(t, uintptr(48), unsafe.Offsetof(attr.cnt))
	require.Equal(t, uintptr(52), unsafe.Offsetof(attr.uprobeFlags))
	require.Equal(t, uintptr(56), unsafe.Offsetof(attr.pid))
	require.Equal(t, uintptr(60), unsafe.Offsetof(attr.pathFd))
}

// TestLinkCreateUprobeMultiBadFd asserts the raw syscall reaches the kernel
// and reports its errno. With a closed prog_fd the kernel answers EBADF, or
// EPERM first when unprivileged BPF is disabled and the test runs without
// CAP_BPF, so both are accepted; neither needs BPF privileges to observe.
func TestLinkCreateUprobeMultiBadFd(t *testing.T) {
	err := linkCreateUprobeMulti(-1, "/", -1)
	require.True(t, errors.Is(err, unix.EBADF) || errors.Is(err, unix.EPERM), "unexpected error: %v", err)
}
