package probe

// bpftime_attach.go — single-uprobe attachment path for bpftime.
//
// bpftime intercepts perf_event_open (for upro creation) and BPF_LINK_CREATE
// (for attaching a BPF program to a perf event), but it silently no-ops
// BPF_LINK_CREATE(BPF_TRACE_UPROBE_MULTI). The classic path that works is:
//
//  1. perf_event_open(uprobe, path, offset)        → bpftime creates bpf_perf_event_handler
//  2. bpf(BPF_LINK_CREATE, {prog, perf, BPF_PERF_EVENT, cookie})
//     → bpftime creates bpf_link_handler with attach_cookie
//     → agent instantiates the link, sets current_thread_bpf_cookie on each call
//     → BPF program sees the per-function cookie via bpf_get_attach_cookie()
//
// libbpfgo does not expose per-cookie single-uprobe attachment, so we drive
// the two syscalls directly using golang.org/x/sys/unix.

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// bpfCmdLinkCreate is BPF_LINK_CREATE.
	bpfCmdLinkCreate = 28

	// bpfAttachTypePerfEvent is BPF_PERF_EVENT (attach_type value 41).
	bpfAttachTypePerfEvent = 41
)

// attachSingleUprobes attaches the probe to each (offset, cookie) pair in
// exePath using single uprobes. It is called in userspace BPF mode instead
// of AttachUprobeMulti.
func (p *Probe) attachSingleUprobes(exePath string, offsets, cookies []uint64) error {
	progFd := p.bpfProg.FileDescriptor()
	if progFd < 0 {
		return fmt.Errorf("bpf program has no file descriptor (not loaded?)")
	}

	uprobeType, err := readUprobeType()
	if err != nil {
		return fmt.Errorf("read uprobe perf type: %w", err)
	}

	var firstErr error
	attached := 0
	for i, offset := range offsets {
		cookie := cookies[i]
		perfFd, linkFd, err := attachUprobeWithCookie(progFd, uprobeType, exePath, offset, cookie)
		if err != nil {
			p.logger.Debug().
				Err(err).
				Uint64("offset", offset).
				Uint64("cookie", cookie).
				Msg("failed to attach single uprobe")
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		p.rawFds = append(p.rawFds, perfFd, linkFd)
		attached++
	}

	if attached == 0 && len(offsets) > 0 {
		return fmt.Errorf("all %d uprobe attachments failed (first error: %w)", len(offsets), firstErr)
	}

	p.logger.Debug().
		Int("attached", attached).
		Int("total", len(offsets)).
		Msg("single uprobes attached")

	return nil
}

// attachUprobeWithCookie opens a uprobe perf event for (exePath, offset) and
// creates a BPF link that carries cookie so bpf_get_attach_cookie() returns it.
// Returns the perf event FD and link FD; both must be kept open.
func attachUprobeWithCookie(progFd int, uprobeType int, exePath string, offset, cookie uint64) (perfFd, linkFd int, err error) {
	// Null-terminate the path and pin it for the duration of the syscall.
	pathBytes := append([]byte(exePath), 0)

	attr := unix.PerfEventAttr{
		Type: uint32(uprobeType),
		Size: uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		// Ext1 = config1 = pointer to binary path string
		Ext1: uint64(uintptr(unsafe.Pointer(&pathBytes[0]))),
		// Ext2 = config2 = function offset in binary
		Ext2: offset,
	}

	perfFd, err = unix.PerfEventOpen(&attr, -1 /* any process */, 0 /* cpu 0 */, -1 /* no group */, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return -1, -1, fmt.Errorf("perf_event_open uprobe offset=%d: %w", offset, err)
	}

	linkFd, err = bpfLinkCreatePerfEvent(progFd, perfFd, cookie)
	if err != nil {
		unix.Close(perfFd)
		return -1, -1, fmt.Errorf("BPF_LINK_CREATE perf_event offset=%d: %w", offset, err)
	}

	return perfFd, linkFd, nil
}

// bpfLinkCreatePerfEvent calls bpf(BPF_LINK_CREATE) to attach progFd to perfFd
// with the given bpf_cookie. bpftime intercepts this and stores attach_cookie
// so the agent can set current_thread_bpf_cookie on each function call.
//
// Layout of union bpf_attr for BPF_LINK_CREATE / perf_event (offsets in bytes):
//
//	[0]  prog_fd       u32
//	[4]  target_fd     u32   (perf event fd)
//	[8]  attach_type   u32   (BPF_PERF_EVENT = 41)
//	[12] flags         u32
//	[16] bpf_cookie    u64   (perf_event union starts here; overlaps target_btf_id)
//
// perf_event.bpf_cookie is the first (and only) field of the perf_event
// member of the anonymous union in bpf_link_create. The union itself starts
// at offset 16, immediately after flags. target_btf_id (u32) also lives at
// offset 16 — they are union members, not sequential fields.
func bpfLinkCreatePerfEvent(progFd, perfFd int, cookie uint64) (int, error) {
	// 24 bytes: prog_fd(4) + target_fd(4) + attach_type(4) + flags(4) + bpf_cookie(8)
	var attr [24]byte
	binary.NativeEndian.PutUint32(attr[0:], uint32(progFd))
	binary.NativeEndian.PutUint32(attr[4:], uint32(perfFd))
	binary.NativeEndian.PutUint32(attr[8:], bpfAttachTypePerfEvent)
	// attr[12] = flags = 0
	binary.NativeEndian.PutUint64(attr[16:], cookie)

	fd, _, errno := unix.Syscall(unix.SYS_BPF,
		bpfCmdLinkCreate,
		uintptr(unsafe.Pointer(&attr[0])),
		uintptr(len(attr)),
	)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

// readUprobeType reads the perf event type for uprobes from sysfs.
// This is the value that must be placed in perf_event_attr.type.
func readUprobeType() (int, error) {
	const typePath = "/sys/bus/event_source/devices/uprobe/type"
	data, err := os.ReadFile(typePath)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w (kernel uprobe support required)", typePath, err)
	}
	var t int
	_, err = fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &t)
	if err != nil {
		return 0, fmt.Errorf("parse uprobe type from %q: %w", string(data), err)
	}
	return t, nil
}
