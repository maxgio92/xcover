package probe

import (
	"runtime"
	"unsafe"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

// ErrPIDFilterByThread reports a kernel whose uprobe_multi PID filter matches
// one thread instead of the whole thread group. Such a kernel lacks upstream
// commit 46ba0e49b642 ("bpf: fix multi-uprobe PID filtering logic"), which
// 6.6.35, 6.9.5 and 6.10 or newer carry.
var ErrPIDFilterByThread = errors.New("the kernel filters uprobe_multi by thread instead of thread group (missing commit 46ba0e49b642)")

// uprobeMultiLinkCreateAttr is the BPF_LINK_CREATE member of union bpf_attr
// as the BPF_TRACE_UPROBE_MULTI attach type reads it: struct link_create and
// its uprobe_multi member in include/uapi/linux/bpf.h (libbpf 1.8 headers).
// The pointer fields are unsafe.Pointer rather than uint64 so the runtime
// keeps their targets alive and updates them if the stack moves before the
// syscall; on 64-bit Linux they have the size and alignment of
// __aligned_u64, and uprobe_multi itself needs a 64-bit kernel. Fields left
// zero must stay zero: the kernel rejects an attr with non-zero bytes past
// the fields it knows.
type uprobeMultiLinkCreateAttr struct {
	progFd        uint32
	targetFd      uint32
	attachType    uint32
	flags         uint32
	path          unsafe.Pointer
	offsets       unsafe.Pointer
	refCtrOffsets unsafe.Pointer
	cookies       unsafe.Pointer
	cnt           uint32
	uprobeFlags   uint32
	pid           uint32
	pathFd        uint32
}

// CheckPIDFilter reports whether the kernel applies the uprobe_multi PID
// filter to the whole thread group. It returns nil on a fixed kernel,
// ErrPIDFilterByThread on a kernel without commit 46ba0e49b642, and another
// error when the outcome was inconclusive. It needs the program Init loaded
// with the uprobe_multi attach type, so it is meant to run between Init and
// Attach in kernel mode; under WithUserspaceBPF the result is meaningless.
//
// It replicates libbpf's probe_uprobe_multi_link (features.c) with the loaded
// program instead of a stub: a link_create for the non-regular path "/" with
// pid -1 fails with EINVAL on a fixed kernel, which rejects the negative pid
// before looking at the path, and with EBADF on a broken one, which only
// reaches the path check. libbpf runs that probe for USDT auto-attach; the
// explicit uprobe_multi attach xcover uses does not consult it.
func (p *Probe) CheckPIDFilter() error {
	return pidFilterVerdict(linkCreateUprobeMulti(p.bpfProg.FileDescriptor(), "/", -1))
}

// pidFilterVerdict maps the outcome of the probing link_create to the
// CheckPIDFilter contract.
func pidFilterVerdict(err error) error {
	switch {
	case err == nil:
		return errors.New("uprobe_multi pid filter check: link_create on path / unexpectedly succeeded")
	case errors.Is(err, unix.EINVAL):
		return nil
	case errors.Is(err, unix.EBADF):
		return ErrPIDFilterByThread
	default:
		return errors.Wrap(err, "uprobe_multi pid filter check: link_create failed")
	}
}

// linkCreateUprobeMulti issues BPF_LINK_CREATE for progFd with one uprobe at
// offset 0 in path, filtered to pid, the way libbpf's bpf_link_create (bpf.c)
// does it, including target_fd -1. A link it does create is closed before
// returning. On failure it returns the errno.
func linkCreateUprobeMulti(progFd int, path string, pid int) error {
	cPath, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}
	offset := uint64(0)

	var attr uprobeMultiLinkCreateAttr
	attr.progFd = uint32(progFd)
	attr.targetFd = ^uint32(0)
	attr.attachType = unix.BPF_TRACE_UPROBE_MULTI
	attr.path = unsafe.Pointer(cPath)
	attr.offsets = unsafe.Pointer(&offset)
	attr.cnt = 1
	attr.pid = uint32(pid)

	fd, _, errno := unix.Syscall(unix.SYS_BPF, unix.BPF_LINK_CREATE, uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	runtime.KeepAlive(&attr)
	if errno != 0 {
		return errno
	}
	_ = unix.Close(int(fd))
	return nil
}
