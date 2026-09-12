// Package e2e holds black-box tests that drive a built xcover binary.
//
// Build with the e2e tag. The tests read the binary path from the
// XCOVER_E2E_BIN environment variable and skip when it is unset, when stale
// /tmp/xcover.{pid,sock} files exist, or when the environment denies BPF
// loading. They must run as root (or with CAP_BPF and CAP_PERFMON), so run the
// compiled test binary under sudo rather than `sudo go test`:
//
//	go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
//	sudo env XCOVER_E2E_BIN="$PWD/xcover" /tmp/xcover-e2e.test -test.v
package e2e
