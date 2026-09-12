// Package e2e holds black-box tests that drive a built xcover binary.
//
// Build with the e2e tag. The tests read the binary path from the
// XCOVER_E2E_BIN environment variable and skip when it is unset, when stale
// /tmp/xcover.{pid,sock} files exist, or when the environment denies BPF
// loading. They must run as root (or with CAP_BPF and CAP_PERFMON), so the
// compiled test binary runs under sudo rather than `sudo go test`. Build
// ./xcover first, then run the target that CI also mirrors (it prompts for
// sudo):
//
//	make xcover
//	make test-e2e
package e2e
