// Package preflight validates the running environment before any BPF object
// is loaded, so that missing privileges surface as an actionable error instead
// of an EPERM deep in the attach path, and kernels that look too old for
// uprobe_multi get an advisory before libbpf's bare "invalid argument".
package preflight

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	log "github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// SkipFlag is the name of the run flag that bypasses the checks.
const SkipFlag = "skip-preflight"

// ErrMissingCapabilities is returned when the effective capability set lacks
// what is needed to load and attach the BPF programs.
var ErrMissingCapabilities = errors.New("missing capabilities")

// MinKernel is the oldest upstream kernel the default (kernel BPF) mode
// supports: BPF_TRACE_UPROBE_MULTI landed in 6.6, and it in turn implies
// bpf_get_attach_cookie for uprobes (5.15) and BPF_MAP_TYPE_RINGBUF (5.8).
//
// It is informational only. Distribution kernels backport uprobe_multi to
// older releases (RHEL 9.4 ships it on 5.14), so a release older than
// MinKernel yields an advisory, not an error.
var MinKernel = Version{Major: 6, Minor: 6}

// Version is a kernel release reduced to the major.minor pair the checks
// compare on.
type Version struct {
	Major int
	Minor int
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// Before reports whether v is older than o.
func (v Version) Before(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}

	return v.Minor < o.Minor
}

// ParseRelease extracts major.minor from a kernel release string as reported
// by uname(2). Distro suffixes are tolerated: "6.12.0-1-amd64" and
// "6.18.44-r0-gcp-6.18" both parse as 6.12 and 6.18. Anything past the minor
// component is ignored: the upstream feature set is keyed on major.minor,
// and the patch level or local tag says nothing reliable about which
// features a distribution backported.
func ParseRelease(release string) (Version, error) {
	fields := strings.SplitN(release, ".", 3)
	if len(fields) < 2 {
		return Version{}, errors.Errorf("unrecognized kernel release %q", release)
	}

	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return Version{}, errors.Wrapf(err, "unrecognized kernel release %q", release)
	}

	// The minor may carry a suffix when there is no patch level ("6.6-rc1").
	minor, err := strconv.Atoi(leadingDigits(fields[1]))
	if err != nil {
		return Version{}, errors.Wrapf(err, "unrecognized kernel release %q", release)
	}

	return Version{Major: major, Minor: minor}, nil
}

// leadingDigits returns the run of ASCII digits s starts with.
func leadingDigits(s string) string {
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		return s
	}

	return s[:end]
}

// CheckKernel reads the running kernel release and returns the detected
// version together with an advisory, non-empty when the release looks older
// than MinKernel. The version number alone cannot tell whether uprobe_multi
// is present, since distributions backport it, so the caller should log the
// advisory rather than fail on it. The error covers uname and parse failures.
func CheckKernel() (Version, string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return Version{}, "", errors.Wrap(err, "failed to read kernel release")
	}

	return checkRelease(unix.ByteSliceToString(uts.Release[:]))
}

// checkRelease parses release and compares it with MinKernel, producing the
// user-facing advisory. Kept separate from the uname call so it is
// unit-testable.
func checkRelease(release string) (Version, string, error) {
	v, err := ParseRelease(release)
	if err != nil {
		return Version{}, "", err
	}
	if v.Before(MinKernel) {
		return v, fmt.Sprintf(
			"kernel %s is older than %s; uprobe_multi needs a distribution backport "+
				"(RHEL 9.4 on 5.14 has one), attach will fail if the backport is missing",
			release, MinKernel), nil
	}

	return v, "", nil
}

// capNames maps the capability bits the checks care about to their names.
var capNames = map[int]string{
	unix.CAP_SYS_ADMIN: "CAP_SYS_ADMIN",
	unix.CAP_BPF:       "CAP_BPF",
	unix.CAP_PERFMON:   "CAP_PERFMON",
}

// missingCapabilities returns the capabilities that must be added to the
// effective set for BPF loading and uprobe attachment to succeed, given the
// two 32-bit effective words of a _LINUX_CAPABILITY_VERSION_3 capget(2).
// CAP_SYS_ADMIN alone is sufficient; otherwise both CAP_BPF and CAP_PERFMON
// are required. A nil result means nothing is missing.
func missingCapabilities(effLow, effHigh uint32) []string {
	has := func(c int) bool {
		if c < 32 {
			return effLow&(1<<uint(c)) != 0
		}

		return effHigh&(1<<uint(c-32)) != 0
	}

	if has(unix.CAP_SYS_ADMIN) {
		return nil
	}

	var missing []string
	for _, c := range []int{unix.CAP_BPF, unix.CAP_PERFMON} {
		if !has(c) {
			missing = append(missing, capNames[c])
		}
	}

	return missing
}

// CheckCapabilities inspects the effective capability set of the calling
// process and returns ErrMissingCapabilities (wrapped) naming what is missing.
// The effective set is consulted rather than the uid, because a root process
// that dropped its capabilities cannot load BPF either.
func CheckCapabilities() error {
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		return errors.Wrap(err, "failed to read process capabilities")
	}

	missing := missingCapabilities(data[0].Effective, data[1].Effective)
	if len(missing) == 0 {
		return nil
	}

	return errors.Wrapf(ErrMissingCapabilities,
		"%s not in the effective set; run with sudo, or grant them to the binary with "+
			"`setcap cap_bpf,cap_perfmon+ep /path/to/xcover` "+
			"(file capabilities do not apply under go run or on nosuid mounts)",
		strings.Join(missing, " and "))
}

// uidMapPath lists the uid mapping of the calling process's user namespace,
// see user_namespaces(7).
const uidMapPath = "/proc/self/uid_map"

// identityUIDMap is the single mapping line of the initial user namespace.
var identityUIDMap = [3]uint32{0, 0, math.MaxUint32}

// isIdentityUIDMap reports whether uidMap, the content of /proc/self/uid_map,
// is the identity mapping "0 0 4294967295". Each line holds three decimal
// fields: the first ID inside the namespace, the first ID outside it and the
// length of the range (user_namespaces(7)). An empty map belongs to a
// namespace whose mapping has not been written yet, so it is not the
// identity either. Lines that do not fit the format are an error.
func isIdentityUIDMap(uidMap string) (bool, error) {
	var mappings [][3]uint32
	for _, line := range strings.Split(uidMap, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			return false, errors.Errorf("unrecognized uid_map line %q", line)
		}

		var m [3]uint32
		for i, f := range fields {
			n, err := strconv.ParseUint(f, 10, 32)
			if err != nil {
				return false, errors.Wrapf(err, "unrecognized uid_map line %q", line)
			}
			m[i] = uint32(n)
		}
		mappings = append(mappings, m)
	}

	return len(mappings) == 1 && mappings[0] == identityUIDMap, nil
}

// inUserNamespace reports whether the process runs in a user namespace other
// than the initial one, judged by /proc/self/uid_map: the initial namespace
// is the one with the identity mapping. A child namespace that a privileged
// parent gave the full identity mapping is not told apart.
func inUserNamespace() (bool, error) {
	uidMap, err := os.ReadFile(uidMapPath)
	if err != nil {
		return false, errors.Wrap(err, "failed to read the uid map")
	}

	identity, err := isIdentityUIDMap(string(uidMap))
	if err != nil {
		return false, err
	}

	return !identity, nil
}

// Options configures Run.
type Options struct {
	skip         bool
	userspaceBPF bool
	logger       log.Logger

	// Check implementations, replaceable in tests.
	checkKernel       func() (Version, string, error)
	checkCapabilities func() error
	inUserNamespace   func() (bool, error)
}

// Option configures Options.
type Option func(*Options)

// WithSkip bypasses every check when skip is true.
func WithSkip(skip bool) Option {
	return func(o *Options) { o.skip = skip }
}

// WithUserspaceBPF marks the bpftime mode, which implies skipping: it does not
// use kernel uprobe_multi and runs unprivileged.
func WithUserspaceBPF(userspace bool) Option {
	return func(o *Options) { o.userspaceBPF = userspace }
}

// WithLogger sets the logger used to report the outcome.
func WithLogger(logger log.Logger) Option {
	return func(o *Options) { o.logger = logger }
}

// Run reports the kernel version advisory, if any, and returns the capability
// check failure. When the capabilities are present it also warns if the
// process runs in a user namespace, where the check cannot see what the
// kernel enforces. A kernel or uid map that cannot be read or parsed is
// logged and otherwise ignored, since both are informational. Checks are
// skipped when requested or when running in userspace BPF mode.
func Run(opts ...Option) error {
	o := &Options{
		logger:            log.Nop(),
		checkKernel:       CheckKernel,
		checkCapabilities: CheckCapabilities,
		inUserNamespace:   inUserNamespace,
	}
	for _, f := range opts {
		f(o)
	}

	if o.skip || o.userspaceBPF {
		o.logger.Debug().Bool("userspace-bpf", o.userspaceBPF).Msg("preflight skipped")
		return nil
	}

	kernel, advisory, err := o.checkKernel()
	switch {
	case err != nil:
		o.logger.Warn().Err(err).Msg("kernel version not checked")
	case advisory != "":
		o.logger.Warn().Msg(advisory)
	}

	if err := o.checkCapabilities(); err != nil {
		return err
	}

	// capget(2) reports the set local to the process's user namespace, while
	// the kernel checks BPF privileges against the initial namespace. A passed
	// check inside a child namespace therefore proves nothing; warn so the
	// EPERM that may follow has an explanation.
	inNS, err := o.inUserNamespace()
	switch {
	case err != nil:
		o.logger.Warn().Err(err).Msg("user namespace not checked")
	case inNS:
		o.logger.Warn().Msg("running in a user namespace: the capability check passed against " +
			"the namespace-local set and may not match what the kernel enforces, " +
			"so the BPF load can still fail with EPERM")
	}

	if kernel != (Version{}) {
		o.logger.Debug().Msgf("preflight ok: kernel %s, capabilities present", kernel)
	} else {
		o.logger.Debug().Msg("preflight ok: capabilities present")
	}

	return nil
}
