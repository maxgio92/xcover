# Documentation audit (2026-09-12)

Research on the xcover project and its documentation, to find gaps, errors and
drift between docs and code. Four parallel passes: CLI surface, architecture
and limitations, build/CI/contributor material, external sources.

## Summary

The README oversold the tool ("revolutionizes", "Production-ready", "Traces
all function calls") and omitted requirements, installation, limitations and
contributor guidance. Several documented behaviours did not match the code.
Two generated pages described commands that no longer exist. The README source
was rewritten, a contributor guide and an architecture page were added, the
doc generator was fixed so links resolve from both the repository root and
`docs/`, and all generated pages were regenerated.

## Findings

### Gaps filled

- **Requirements.** Default mode needs Linux 6.6+ for `uprobe_multi` links
  [1][2], root or `CAP_BPF` + `CAP_PERFMON` [3][4], and BTF only to build [5].
  Issue #66 says 5.17 is enough; `uprobe_multi` landed in 6.6 [1][6].
- **Install.** GoReleaser is configured to publish tarballs [7], but all seven
  releases have zero assets because the release workflow failed on every tag
  [8][9].
- **Limitations.** Inlined functions are invisible [10]; only the first hit per
  function is recorded [11]; the kernel map caps at 40960 functions [11]; one
  daemon per host because state is fixed under `/tmp` [12]; project scope is
  Go only and falls back silently [13][14].
- **Overhead.** About 1230 ns per probed call versus 1.2 ns unprobed [15][16];
  a sub-second run tracing 15k functions took close to a minute [17].
- **Contributor guide.** No CONTRIBUTING, commit convention, test layers or
  docs regeneration duty. CI runs only `test-integration`, `e2e` under sudo,
  `gofmt` and `go mod verify` [18].

### Code and docs disagreed

- `--pid` is parsed but never read; attach always uses PID -1 [19][20].
- The README's userspace section existed only in the generated `README.md`,
  not in `README.md.tpl`; `make docs` would have deleted it [21].
- Quickstart used `xcover_report.json`; the real name is `xcover-report.json`
  [22]. `stop` prints `xcover stopped (PID n)`, not `xcover is stopped` [23].
- Project scope docs said module filtering runs before include/exclude; the
  code does the reverse [13].
- `docs/xcover_profile.md` and `docs/xcover_start.md` documented removed
  commands [24].
- `docs/xcover_run.md` lacked `--userspace-bpf` [25].
- Generated SEE ALSO links only resolved from the repository root [26].
- "Check the the" typo in the cobra `Short` string [27].
- Demo setup page referenced renamed files and a placeholder asciinema ID [28].

### Code defects surfaced (not documentation)

1. `--pid` never applied (`pkg/cmd/run/run.go:57`, `pkg/probe/probe.go:175`).
2. Failed batch attach is logged and swallowed; readiness is still reported
   (`pkg/probe/probe.go:177`).
3. `funcs_ack` can be truncated: the `Range` callback returns false on an
   unknown cookie (`pkg/trace/tracer.go:307`). `cov_by_func` uses the raw ack
   count and can disagree with `len(funcs_ack)`. Related to issue #175.
4. `bpf_printk` fires on every hit, including the fast path
   (`bpf/trace.bpf.c:32`).
5. `bpf_map_update_elem` on `seen_funcs` is unchecked; past 40960 entries every
   call emits an event (`bpf/trace.bpf.c:42`).
6. Include and exclude regexes are compiled once per symbol, and an invalid
   pattern panics (`pkg/trace/resolver.go:193,197`).
7. Report `os.Create` error is logged but not returned (`pkg/trace/tracer.go:322`).
8. The release workflow fails on tag push, so no binaries ship [9].
9. CI does not check that `README.md` and `docs/` are regenerated.
10. `Makefile` derives `-D__TARGET_ARCH_aarch64` on arm64 hosts; libbpf expects
    `arm64` (`Makefile:14,22`). Harmless today because the BPF program uses no
    `PT_REGS` macros.

## Sources

[1] https://kernelnewbies.org/Linux_6.6 - multi uprobe link added in 6.6
[2] https://lwn.net/Articles/930066/ - uprobe_multi patch series
[3] https://man7.org/linux/man-pages/man7/capabilities.7.html - CAP_BPF, CAP_PERFMON
[4] benchmark/README.md - sudo needed for CAP_BPF + CAP_PERFMON
[5] Makefile:112-125 - vmlinux.h generation requires BTF
[6] https://github.com/maxgio92/xcover/issues/66 - claims 5.17 requirement
[7] .goreleaser.yml - archive names and targets
[8] https://github.com/maxgio92/xcover/releases - releases without assets
[9] https://github.com/maxgio92/xcover/actions/runs/27408053440 - failed release run for 0.5.1
[10] https://github.com/maxgio92/xcover/issues/51 - inlined functions
[11] bpf/trace.bpf.c - seen_funcs map, first-hit dedup
[12] internal/settings/settings.go - fixed /tmp paths
[13] pkg/trace/resolver_go.go - project scope order and fallback
[14] https://github.com/maxgio92/xcover/issues/116 - silent scope fallback
[15] benchmark/README.md - hit vs baseline overhead
[16] docs/talks/opensouthcode-2026/slides.md - measured numbers, positioning
[17] https://github.com/maxgio92/xcover/issues/100 - 15k functions, about one minute
[18] .github/workflows/ci.yml - jobs and commands
[19] pkg/cmd/run/run.go:57 - --pid flag definition
[20] pkg/probe/probe.go:175 - attach with pid -1
[21] README.md.tpl versus README.md before the rewrite
[22] pkg/trace/tracer.go:28 - report file name
[23] pkg/cmd/stop/stop.go:65 - stop output
[24] pkg/cmd/cmd.go:52-56 - registered commands
[25] pkg/cmd/run/run.go:70 - --userspace-bpf flag
[26] docs/docs.go before the change - link handler
[27] pkg/cmd/status/status.go:22 - Short string
[28] demo/DEMO_SETUP.md (now demo/README.md) before the change - stale file names
[29] https://raw.githubusercontent.com/golang/go/master/src/debug/gosym/pclntab.go - pclntab formats since Go 1.2
[30] https://github.com/eunomia-bpf/bpftime - bpftime runtime
[31] https://man7.org/linux/man-pages/man8/ld.so.8.html - LD_PRELOAD ignored in secure-execution mode
