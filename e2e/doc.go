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
// rather than `sudo go test`. CI builds xcover with -tags e2etest so the
// drops scenario can cap the seen_funcs map through
// XCOVER_E2E_SEEN_FUNCS_MAX; a binary built without the tag ignores the
// variable and that scenario fails:
//
//	go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
//	sudo env XCOVER_E2E_BIN="$PWD/xcover" XCOVER_E2E_REQUIRE=1 /tmp/xcover-e2e.test -test.v
//
// # Feature matrix
//
// Every run flag, command and failure path has a row below. A row names the
// test that covers it, or the reason it has no e2e scenario. "Every session
// test" means each test built on runXcoverSession or startXcoverDaemon in
// project_scope_test.go.
//
// Run flags:
//
//	--path                 every session test
//	--pid                  TestPIDFilterRestrictsToProcess (pid_filter_test.go)
//	--scope=project        TestProjectScopeFiltersToGoModule (project_scope_test.go)
//	--scope=binary         TestBinaryScopeRetainsNonProjectSymbols (project_scope_test.go)
//	--report               every runXcoverSession test: readReport reads the report the daemon writes on stop (TestStatusReportsRunningThenStopped and TestStaleProcessFileIsOverwritten never read it)
//	--status               every session test: startXcoverDaemon passes --status=false
//	--detach               every session test: startXcoverDaemon passes --detach
//	--include              TestIncludeFilterTracesOnlyMatchingFunctions (filter_test.go)
//	--exclude              TestExcludeFilterDropsMatchingFunctions (filter_test.go)
//	--debug-path           TestDebugPathTracesStrippedBinary (debug_path_test.go)
//	--no-build-id-check    TestDebugPathTracesStrippedBinary (debug_path_test.go)
//	--verbose              TestVerboseLogsFunctionNames (verbose_test.go)
//	--skip-preflight       no e2e: unit tests in internal/preflight
//	--ringbuf-size         TestRingBufSizeSmall and TestRingBufSizeOnePageWarnsDrops (ringbuf_size_test.go); the one-page scenario pauses the daemon with SIGSTOP while the fixture runs so the page fills, and skips with plain t.Skipf on pages above 4 KiB, so XCOVER_E2E_REQUIRE=1 does not fail it
//	--userspace-bpf        no e2e: not in CI, userspace builds only
//
// Commands:
//
//	run                    every session test (startXcoverDaemon)
//	stop                   every session test (stopXcoverDaemon); TestStatusReportsRunningThenStopped (daemon_lifecycle_test.go)
//	wait                   every session test: startXcoverDaemon runs `wait --timeout=15s`
//	status                 TestStatusReportsRunningThenStopped (daemon_lifecycle_test.go)
//	merge                  TestMergeCombinesTwoSessionReports (merge_test.go)
//	agent extract          no e2e: not in CI, userspace builds only
//
// Failure paths:
//
//	stop force-kill        no e2e: unit-covered by pkg/cmd/stop tests (ErrForceKilled, exit 1); a mid-drain daemon cannot be timed deterministically in e2e
//	stale PID file         TestStaleProcessFileIsOverwritten (daemon_lifecycle_test.go)
//	merge build_id missing TestMergeAllowsMissingBuildID (merge_test.go)
//	attach failure         TestAttachFailureExitsBeforeReady (attach_failure_test.go): a --debug-path pair whose executable is truncated past the included function's offset, so uprobe_register returns EINVAL before readiness
//	recovery filter refusal TestRecoveryRefusesFiltersBeforeReady (recovery_filter_test.go): a stripped C++ fixture with neither .symtab nor .gopclntab run with --include, so Init fails with ErrFilterNeedsSymbols before readiness instead of recovering func_0x<offset> names that the pattern cannot match
//	seen_funcs drops warning TestDropsWarningReportsRejectedInserts (drops_warning_test.go): XCOVER_E2E_SEEN_FUNCS_MAX=1 caps the map to one function on a binary built with -tags e2etest, so the second distinct function hit drops and the log carries a positive dropped count
//	--pid thread-filter warning TestPIDFilterWarnsOnThreadFilteringKernel (pid_filter_warning_test.go); skips with plain t.Skipf on a kernel that carries commit 46ba0e49b642, so XCOVER_E2E_REQUIRE=1 does not fail it; an inconclusive check fails in both cases
//
// C++ name demangling is covered by TestCppReportCarriesDemangledNames
// (demangle_test.go), which skips when g++ is absent.
package e2e
