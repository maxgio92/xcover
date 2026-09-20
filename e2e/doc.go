// Package e2e holds black-box tests that drive a built xcover binary.
//
// Build with the e2e tag. The tests read the binary path from the
// XCOVER_E2E_BIN environment variable and skip when it is unset, when stale
// /tmp/xcover.{pid,sock} files exist, or when not running as root, unless
// XCOVER_E2E_REQUIRE=1 is set, in which case those preconditions fail the
// test instead (CI sets it). Any xcover error past the preconditions is a
// failure, never a skip. The tests build their fixture programs with go
// build. Set XCOVER_E2E_FIXTURES to a directory holding one prebuilt binary
// per scenario to skip that step (make e2e-fixtures produces it). The
// harness checks for euid 0, so run the compiled test binary under sudo
// rather than `sudo go test`:
//
//	go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
//	sudo env XCOVER_E2E_BIN="$PWD/xcover" XCOVER_E2E_REQUIRE=1 /tmp/xcover-e2e.test -test.v
package e2e
