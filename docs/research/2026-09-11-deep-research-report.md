# xcover: state of the project, the uprobe coverage field, and a path to maturity

## Summary

xcover measures function-level test coverage of any ELF binary by attaching one eBPF uprobe per function through a `uprobe_multi` link, with no rebuild of the target. Research covered the code (two audits on the current tree at 66a7754), the kernel uprobe field, competing tools, dependency health, and market direction. Conclusion: the approach is technically sound and has no direct open-source competitor, but the implementation has correctness bugs that can silently yield a wrong report (swallowed attach failures, a shutdown deadlock, phantom functions at offset 0, a dead `--pid` flag), the project is single-maintainer with 22 stars and no downstream importers, and its README overstates maturity. The strongest growth path is not "better coverage tool" but "open-source function-level runtime reachability": the same hit map that produces coverage is what security vendors now sell to decide whether a vulnerable function ever executed, and OpenVEX already has the justification slot to consume it.

## Findings

### 1. Bugs that produce a wrong or missing report

KEY: Several failure paths end with exit code 0 and a plausible-looking report.

- **Attach failures are swallowed.** `probe.Attach` logs a warning and returns nil on `AttachUprobeMulti` error; `Run` then signals readiness. On a kernel below 6.6, or with one bad offset in a 128-offset batch, `xcover wait` prints "ready", tests run untraced, and the report shows 0% coverage [A1][B6]. libbpf's own "failed to attach multi-uprobe" warning is downgraded to debug by the log filter [A1].
- **Shutdown can deadlock and drops tail events.** `ingestEvents` does an unconditional `feed <- data`; `processEvents` may exit on `ctx.Done()` with the feed channel full. `wg.Wait()` then never returns, no report is written, and `xcover stop` force-kills after 5 seconds and returns success [A2][A4]. Events still buffered at cancellation are discarded in every case, so functions called at the end of a test are undercounted [A2].
- **Undefined imports become phantom functions.** On PIE binaries the first `PT_LOAD` starts at VA 0, so `SHN_UNDEF` symbols such as `puts@GLIBC` (STT_FUNC, Value 0) map to file offset 0 and are attached and counted in the denominator. Every dynamically linked C, C++ or Rust PIE binary carries at least one function that can never fire. `resolver_debug.go` already filters this case, but only on the `--debug-path` path [B1].
- **`--pid` is dead.** The flag is parsed, documented in the README, and forwarded to the daemon, but the attach calls hard-code pid -1. Any other process running the same binary during the session inflates coverage [A8][B2]. The kernel side also had a thread-vs-process filter bug in 6.6 to 6.9, fixed by commit 46ba0e49b642 in May 2024 [17].
- **Report truncation on unknown cookies.** `writeReport`'s `Range` callback returns false on a cookie miss, which aborts iteration, while `cov_by_func` still counts it. A single decode error acks cookie 0 and triggers this. Already tracked as issue #175 [A9][B7][26].
- **Fixed `/tmp` paths.** PID file, socket and report path are global. A second `run` deletes a live daemon's socket; a non-root `status` treats EPERM as "not running"; `stop` sends SIGKILL to whatever PID sits in a world-writable file without checking `/proc/PID/exe` [A3].
- **Daemon init failure leaves a stale socket.** `wait` then spins for the full 120 second timeout because liveness is checked once before the loop [A5].
- **`seen_funcs` is capped at 40960 entries.** Larger binaries lose in-kernel dedup, and because the map is marked before `bpf_ringbuf_reserve`, a reserve failure permanently loses that function [A6].
- **Invalid `--include` regex panics inside the daemon** after the parent has exited 0, because `regexp.MustCompile` runs per symbol at resolve time [B5].
- **Go project scope drops root-package functions when the module path's last element contains a dot** (`gopkg.in/yaml.v3` becomes `gopkg.in/yaml%2ev3.` in symbol names) [B3].

### 2. Incorrectness in what the tool claims to measure

- CAVEAT: **Inlining is undocumented.** On a default Go build a fixture with 8 project functions resolves to 2; the rest are inlined and have no symbol to probe. The project's own e2e fixture works only because every function is `//go:noinline` [B4]. Instrumented tools (Go `-cover`, Clang source-based coverage) insert counters before inlining and do not have this blind spot [30][31]. This is a structural limit of the approach and must be stated as such.
- **Only the main executable is probed.** Functions in shared-library dependencies, IFUNC resolvers and PLT stubs never appear [B11].
- **Symtab and pclntab disagree on names** (`.abi0` suffix on assembly functions), so filters behave differently on stripped and unstripped builds of the same program; zero-size markers like `runtime.etext` are probed; aliased offsets report an arbitrary name [B8].
- **"Production-ready, minimal overhead" versus measured cost.** Each hit is an int3 trap plus single-step. Independent measurements range from ~300 ns per hit single-threaded on 6.12+ [16] to 850-1550 ns on 5.4-era kernels [20], and ~1.7 us on 6.7-rc3 [12]. The project's own benchmark README shows the hit path at hundreds of times baseline. Three `bpf_printk` calls sit on the hot path [A7].
- **Kernel floor is unstated.** `uprobe_multi` needs Linux 6.6 [13][14], `bpf_get_attach_cookie` 5.15 [15], ringbuf 5.8. Ubuntu 22.04 (5.15), Debian 12 (6.1), RHEL 9 (5.14) and Amazon Linux 2023 before August 2026 (6.1) all fail with a bare EINVAL [22]. CONFLICT: docs.ebpf.io lists 5.15+ for `attach_uprobe_multi`; kernel changelogs show 6.6. Trust 6.6 [14].
- **Capabilities are understated.** README says CAP_BPF; tracing programs also need CAP_PERFMON [23]. Vendors disagree too (Beyla lists CAP_SYS_ADMIN for uprobes, Odigos CAP_BPF plus CAP_SYS_PTRACE) [32][33].

### 3. Tests, CI, docs, release

- CI e2e skips on error text containing "failed to load bpf object" or "permission denied", and asserts only `funcs_traced`, never `funcs_ack`. A regression where no event reaches userspace stays green [B9].
- Untested: attach failure, pipeline drain, `writeReport`, `stop` grace and kill, `wait` against a dead daemon [A18].
- `make docs` regenerates README from a template that lacks the Userspace BPF section, so the next run deletes it [B10]. `docs/xcover_profile.md` and `xcover_start.md` document commands that no longer exist. Quickstart references `xcover_report.json`; the file is `xcover-report.json` [B11][A4].
- Release: both goarch targets write the same `trace.bpf.o` in concurrent pre-hooks; `-lelf -lz` are linked dynamically while docs imply a static binary; `-X main.version` targets variables that do not exist [B13]. Tags lack the `v` prefix, so pkg.go.dev shows a pseudo-version [1].

### 4. Where the field is going

- **Kernel uprobes got much faster, but not for function entries.** 6.12 added SRCU and lockless lookup, 6.13 RCU Tasks Trace, 6.14 speculative VMA lookup, and 6.11 a uretprobe syscall [16][18]. 6.18 adds "optimized uprobes" that rewrite a 5-byte NOP into a call to a trampoline, 7 M/s versus 1.1 M/s, but only when the probed instruction is a nop5, which is USDT sites, not function prologues [19]. xcover stays on the int3 path. CONFLICT: session uprobes landed in 6.13, not 6.12 as sometimes reported [14].
- **uretprobes crash Go** (golang/go#22008 open since 2017); the industry workaround is uprobes on RET instructions [25][35]. xcover uses entry probes only and is safe, but should say so.
- **Go 1.26 changed binary layout**: pcHeader no longer records text start, moduledata moved to `.go.module`, `.gosymtab` removed [27]. xcover passes the ELF `.text` address to `gosym`, so it is unaffected, but any future parser change must keep doing that.
- **bpftime** (userspace mode) is active, 1.5k stars, v0.9.0 in August 2026, but its roadmap is GPU-heavy, `uprobe_multi` has been an open idea since 2024, and there are open ring-buffer data-loss reports under multithreading [7][8]. Treat it as an experiment, not a pillar.
- **libbpfgo vs cilium/ebpf.** libbpfgo pins a `-dev` libbpf and carries cgo, static-link and arm64 issues [4]. cilium/ebpf has had `UprobeMulti` with cookies since v0.13.0 (February 2024, written by the kernel feature's author), runtime feature probes, and BPF token support in v0.22.0, at the cost of frequent breaking API changes [5][6].

### 5. Competitive position

No tool ships CI-oriented function coverage from symbols on `uprobe_multi`. Closest neighbours: kcov (ptrace, line-level, needs DWARF) [28], DynamoRIO drcov (DBI, basic block, target runs under DynamoRIO) [29], Intel PT tools (fuzzing-oriented, Intel only) [34]. bpfcov measures coverage of eBPF programs, not of userland, and is a naming collision to disambiguate from [36]. Against instrumented coverage, xcover's differentiators are: exact release artifact, stripped binaries, any language. Its weaknesses are: function granularity, inlining blindness, Linux plus privileges only [30][31].

### 6. Demand signals to build toward

KEY: **Function-execution telemetry from eBPF is now a named security category.** Latio's 2025 taxonomy separates "loaded" (library mapped) from "executed" and "function-level executed", and names Raven, Miggo, Oligo and Kodem as function-level vendors [37]. Kodem: "Static reachability predicts code could execute, while runtime evidence confirms code did execute" [38]. Raven sells "function-level runtime reachability" via eBPF at under 0.2% CPU [39]. Sysdig and ARMO stop at package-loaded granularity [40][41].

- **OpenVEX has the slot.** `vulnerable_code_not_in_execute_path` is a standard justification; Chainguard's `vexctl` creates and attests it; OWASP dep-scan and Endor Labs fill it from static call graphs only [42][43][44]. No vendor found emits VEX from runtime evidence. A report of "vulnerable symbol never hit across the test and soak run" is exactly that evidence.
- **Kubernetes e2e coverage pain is real and unsolved.** Kueue (May 2026), Kubernetes and Istio issues all describe rebuilding `-cover` binaries, exit-only flush, copying files out of pods, and running coverage only in periodics [45][46]. A node-level eBPF collector on release images sidesteps all four.
- **Report formats.** GitHub's native PR coverage (public preview May 2026) takes Cobertura [47]. LCOV FN/FNDA can express function-only data, but Codecov's parser discards FNDA and needs DA lines [48][49]. Teamscale is the one consumer that natively works at method granularity and accepts production coverage [50].
- **Detach-on-first-hit.** UnTracer showed coverage-guided tracing where breakpoints are removed once hit drops overhead below 1% over time [51]. xcover already early-returns in BPF after the first hit; detaching the uprobe removes the trap too and makes long soak and production runs viable.
- **Fuzzing feedback is not a fit.** At ~1 us per hit versus 30k to 120k exec/s Frida baselines, always-on uprobes are 2 to 3 orders of magnitude too slow [52].

## Recommendations

Ordered by what keeps the project alive first.

1. **Fix the silent-wrong-report class now** (release 0.6.0): fail `Run` before readiness when attach fails; drain and exit the pipeline on cancel; filter `SHN_UNDEF` and `Value == 0` in `funcSymsFromELF`; either wire `--pid` through to the attach call or delete it; fix the `Range` early return (#175); compile regexes once; size `seen_funcs` from the function count; return non-zero from `stop` on force kill and make the grace period a flag.
2. **Make the report trustworthy and reproducible**: sort lists, add `schema_version`, build-id, per-function offset and hit count, a `pid` field, and kernel and xcover versions. Add a preflight that checks kernel 6.6 and capabilities and prints a clear error.
3. **Make CI catch kernel regressions**: assert `funcs_ack` in e2e; skip only on a real non-root check. Add unit tests for attach failure, drain, and report writing.
4. **Tell the truth in docs**: a Limitations section (kernel 6.6+, CAP_BPF plus CAP_PERFMON, main executable only, non-inlined functions only, one name per aliased offset, per-hit trap cost with numbers). Remove "production-ready". Fix the template drift, stale command docs and the report filename. Tag releases with a `v` prefix.
5. **Ship LCOV and Cobertura exporters** with function start lines from DWARF or pclntab, and synthesized DA lines for the function's first line so Codecov, Coveralls and GitHub PR coverage accept them. Keep JSON as the lossless format. Land the pending merge PR (#153) so multi-run aggregation works.
6. **Add a runtime-reachability mode**: input a list of vulnerable symbols (from govulncheck or OSV data), output hit or not-hit, and emit an OpenVEX statement with `vulnerable_code_not_in_execute_path` for the misses. This is the feature that puts the project in a growing category rather than a crowded one, and it fits the maintainer's employer's tooling.
7. **Add detach-on-first-hit** as an option for soak and production use, and a Kubernetes DaemonSet story that attaches by `/proc/PID/root/<path>` and filters by mount namespace in BPF (uprobes are inode-keyed, so containers sharing an image share probes) [53][54].
8. **Reduce dependency risk**: evaluate migrating to cilium/ebpf (no cgo, uprobe_multi cookies, feature probes, BPF token); add a per-function legacy uprobe fallback for kernels 5.15 to 6.5; keep bpftime strictly behind the build tag.
9. **Later**: RET-instruction uprobes for per-call duration, C++ and Rust demangling in reports, shared-library coverage via `--path` on `.so` files, and per-test attribution by segmenting the hit stream on markers from an `xcover mark` command.

## Interesting Findings

- The audit fixture proved a default Go build exposed 2 of 8 project functions to uprobes; the e2e suite hides this by marking every fixture function `//go:noinline` [B4].
- The 128-offset batch size rests on a false premise. libbpf passes offsets by pointer; the kernel limit is 1,048,576 per link. A 100k-function binary today creates ~800 links [A12][13].
- The kernel's 6.18 fast uprobe path will not help xcover at all, because function entries are not 5-byte NOPs [19].
- Codecov's documentation lists LCOV as supported, but the parser source skips FNDA, so function-only LCOV uploads produce no coverage [49].

## Open Questions

- Whether `uprobe_multi` links need CAP_PERFMON in addition to CAP_BPF on current kernels was not verified by experiment; sources conflict [23][32][33].
- Whether the 6.6 LTS branch received the PID-filter fix (46ba0e49b642) was not confirmed [17].
- The OpenSouthCode 2026 talk is not indexed beyond the schedule page, so no external reception could be measured [2].

## Sources

Code audit references: [A n] is finding n of the BPF, tracer and daemon audit; [B n] is finding n of the symbol-resolution audit. Both audits ran on this worktree at commit 66a7754 with file and line evidence.

[1] pkg.go.dev xcover: https://pkg.go.dev/github.com/maxgio92/xcover (pseudo-version, zero importers)
[2] OpenSouthCode 2026 proposal: https://www.opensouthcode.org/conferences/opensouthcode2026/program/proposals/1088 (talk abstract)
[3] xcover GitHub API: https://api.github.com/repos/maxgio92/xcover (22 stars, 3 forks, contributors)
[4] libbpfgo releases: https://api.github.com/repos/aquasecurity/libbpfgo/releases?per_page=8 (cadence, -dev libbpf pin)
[5] cilium/ebpf v0.13.0: https://github.com/cilium/ebpf/releases/tag/v0.13.0 (uprobe multi with cookies)
[6] cilium/ebpf link package: https://pkg.go.dev/github.com/cilium/ebpf/link (UprobeMultiOptions.Cookies, v0.22.0)
[7] bpftime issue #214: https://github.com/eunomia-bpf/bpftime/issues/214 (uprobe_multi unsupported since 2024)
[8] bpftime open uprobe issues: https://api.github.com/search/issues?q=repo:eunomia-bpf/bpftime+is:issue+is:open+uprobe (attach failures, ringbuf data loss)
[9] bpftime releases: https://api.github.com/repos/eunomia-bpf/bpftime/releases?per_page=3 (v0.9.0 2026-08-14, GPU focus)
[10] OSDI '25 bpftime paper: https://www.usenix.org/conference/osdi25/presentation/zheng-yusheng (academic basis)
[11] xcover FOSDEM 2026 slides: https://archive.fosdem.org/2026/events/attachments/CNPVJL-lightning_lightning_talks_1/slides/267016/xcover_c_2wnfgpz.pdf (architecture, 1M offset limit)
[12] Cloudflare ebpf_exporter benchmark: https://github.com/cloudflare/ebpf_exporter/blob/master/benchmark/README.md (~1.7 us uprobe cost on 6.7-rc3)
[13] kernel/trace/bpf_trace.c: https://raw.githubusercontent.com/torvalds/linux/master/kernel/trace/bpf_trace.c (MAX_UPROBE_MULTI_CNT, pid validation)
[14] ebpf-docs BPF_LINK_CREATE: https://github.com/isovalent/ebpf-docs/blob/master/docs/linux/syscall/BPF_LINK_CREATE.md (uprobe_multi v6.6, session v6.13)
[15] ebpf-docs bpf_get_attach_cookie: https://github.com/isovalent/ebpf-docs/blob/master/docs/linux/helper-function/bpf_get_attach_cookie.md (v5.15)
[16] Nakryiko RCU uprobe series: https://www.mail-archive.com/linux-kernel@vger.kernel.org/msg2572899.html (M/s throughput numbers)
[17] Commit 46ba0e49b642: https://github.com/torvalds/linux/commit/46ba0e49b64232adac35a2bc892f1710c5b0fb7f (multi-uprobe PID filter bug)
[18] KernelNewbies 6.11: https://kernelnewbies.org/Linux_6.11 (uretprobe syscall)
[19] Olsa optimized uprobes v6: https://www.mail-archive.com/linux-trace-kernel@vger.kernel.org/msg10387.html (nop5-only fast path, 6.18)
[20] Piotrowski AsiaBSDCon 2024: https://papers.freebsd.org/2024/asiabsdcon/piotrowski-Benchmarking-Performance-Overhead-of-DTrace-on-FreeBSD-and-eBPF-on-Linux.files/piotrowski-Benchmarking-Performance-Overhead-of-DTrace-on-FreeBSD-and-eBPF-on-Linux-paper.pdf (per-hit ns on 5.4)
[21] libbpf v1.3.0: https://github.com/libbpf/libbpf/releases/tag/v1.3.0 (attach_uprobe_multi)
[22] AL2023 kernel guide: https://docs.aws.amazon.com/linux/al2023/ug/kernel-update.html (6.1 default until 2026-08-17); Ubuntu lifecycle: https://ubuntu.com/kernel/lifecycle
[23] Kernel perf security: https://docs.kernel.org/admin-guide/perf-security.html (CAP_PERFMON)
[24] CONFIG_BPF_UNPRIV_DEFAULT_OFF: https://cateee.net/lkddb/web-lkddb/BPF_UNPRIV_DEFAULT_OFF.html (unprivileged BPF off)
[25] golang/go #22008: https://github.com/golang/go/issues/22008 (uretprobes unsupported in Go)
[26] xcover open issues: https://github.com/maxgio92/xcover/issues (#175, #51, #100, #116)
[27] Go 1.26 release notes: https://go.dev/doc/go1.26 (pclntab and .go.module changes)
[28] kcov: https://github.com/SimonKagstrom/kcov (ptrace line coverage)
[29] DynamoRIO drcov: https://dynamorio.org/page_drcov.html (basic-block coverage)
[30] Go build -cover: https://go.dev/doc/build-cover (integration coverage since 1.20)
[31] Clang source-based coverage: https://clang.llvm.org/docs/SourceBasedCodeCoverage.html (inlining-immune mapping)
[32] Beyla security: https://grafana.com/docs/beyla/latest/security/ (capability list for uprobe tools)
[33] Odigos Go eBPF: https://docs.odigos.io/instrumentations/golang/ebpf (CAP_BPF + CAP_SYS_PTRACE)
[34] honggfuzz feedback docs: https://github.com/google/honggfuzz/blob/master/docs/FeedbackDrivenFuzzing.md (Intel PT modes)
[35] OTel Go instrumentation how-it-works: https://github.com/open-telemetry/opentelemetry-go-instrumentation/blob/main/docs/how-it-works.md (RET-uprobe pattern)
[36] bpfcov: https://github.com/elastic/bpfcov (covers eBPF programs, not userland)
[37] Latio runtime reachability: https://thehackernews.com/expert-insights/2025/07/everything-to-know-about-runtime.html (loaded vs executed vs function-level)
[38] Kodem: https://www.kodemsecurity.com/resources/from-reachability-to-reality-proving-vulnerable-code-was-executed-exploited-in-production (eBPF function-execution evidence)
[39] Raven Runtime SCA: https://raven.io/runtime-sca (function-level eBPF, overhead)
[40] Sysdig in-use: https://www.sysdig.com/blog/smarter-vulnerability-management-with-in-use-prioritization (package-loaded granularity)
[41] ARMO vulnerabilities in use: https://hub.armosec.io/docs/vulnerabilities-in-use (file-activity eBPF)
[42] OpenVEX spec: https://github.com/openvex/spec/blob/main/OPENVEX-SPEC.md (vulnerable_code_not_in_execute_path)
[43] Chainguard vexctl: https://edu.chainguard.dev/open-source/sbom/getting-started-openvex-vexctl/ (VEX creation and attestation)
[44] OWASP dep-scan reachability: https://depscan.readthedocs.io/reachability-analysis/ (static reachability to VEX)
[45] Kueue coverage issue: https://github.com/kubernetes-sigs/kueue/issues/11173 (e2e coverage pain, 2026-05)
[46] mgasch Go e2e coverage in Kubernetes: https://www.mgasch.com/2023/02/go-e2e/ (exit-only flush in pods)
[47] GitHub PR coverage preview: https://github.blog/changelog/2026-05-26-code-coverage-in-pull-requests-is-now-in-public-preview/ (Cobertura upload)
[48] geninfo(1): https://manpages.debian.org/unstable/lcov/geninfo.1.en.html (FN/FNDA, 2.2 FNL/FNA)
[49] Codecov lcov parser: https://github.com/codecov/worker/blob/main/services/report/languages/lcov.py (FNDA discarded)
[50] Teamscale TGA: https://docs.teamscale.com/reference/test-gap-analysis/ (method granularity, production coverage)
[51] UnTracer: https://arxiv.org/abs/1812.11875 (detach-on-hit coverage tracing)
[52] LibAFL Frida throughput: https://github.com/AFLplusplus/LibAFL/issues/3395 (30k-120k exec/s)
[53] bpfman container attach: https://bpfman.io/v0.5.4/blog/2024/02/26/technical-challenges-for-attaching-ebpf-programs-in-containers/ (mount namespace handling)
[54] Inspektor Gadget uprobe RFE: https://github.com/inspektor-gadget/inspektor-gadget/issues/1912 (inode-shared probes across containers)
[55] Grafana Beyla donation to OTel: https://grafana.com/blog/2025/05/07/opentelemetry-ebpf-instrumentation-beyla-donation/ (OBI trend)
[56] Kakkoyun FOSDEM 2026: https://kakkoyun.me/posts/fosdem-2026-auto-instrumenting-go/ (Go uprobe hazards)
