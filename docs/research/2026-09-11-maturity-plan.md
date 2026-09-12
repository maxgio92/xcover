# xcover maturity plan

Derived from the deep-research report of 2026-09-11 (see
`2026-09-11-deep-research-report.md`). Items reference the report's
finding tags: [A n] BPF, tracer and daemon audit; [B n] symbol-resolution
audit; [n] external source.

Status legend: `[ ]` pending, `[~]` in progress, `[x]` done, `[-]` dropped.

## Phase 1: stop producing silent wrong reports (release 0.6.0)

Each group is one scoped PR with atomic commits.

### PR 1: tracer lifecycle (`fix/tracer-lifecycle`)

- [ ] Return errors from `probe.Attach` and `attachProbe`; fail `Run` before
      `NotifyReadiness` when any batch fails to attach [A1][B6].
- [ ] Remove the feed relay or make its send select on `ctx.Done()`; drain
      buffered events on cancel before returning [A2].
- [ ] Shut the health listener down on every `Init` error path [A5].
- [ ] `stop`: configurable grace period, non-zero exit on force kill [A4].
- [ ] `wait`: re-check daemon liveness inside the polling loop [A5].
- [ ] Unit tests: attach failure, pipeline drain, stop and wait paths [A18].

### PR 2: symbol resolution (`fix/symbol-resolution`)

- [ ] Skip `SHN_UNDEF` and `Value == 0` symbols in `funcSymsFromELF` [B1].
- [ ] Compile include and exclude regexes once and return an error on an
      invalid pattern [B5].
- [ ] Escape the module path like the Go linker (`.` to `%2e` in the last
      element) in `filterByModulePath` [B3].
- [ ] Skip zero-size end markers such as `runtime.etext` [B8].
- [ ] Tests: synthetic `SHN_UNDEF` symbol, PIE C fixture asserting no
      offset 0, `gopkg.in/yaml%2ev3` case, invalid regex.

### PR 3: report correctness and schema (`fix/report-schema`)

- [ ] `writeReport`: skip unknown cookies instead of aborting `Range`;
      compute the percentage from the resolved set [A9][B7] (issue #175).
- [ ] `handleEvent`: return early on decode error [A9].
- [ ] Return the `os.Create` error immediately [A13].
- [ ] Sort `funcs_traced` and `funcs_ack` [A10].
- [ ] Add `schema_version`, `build_id`, `kernel`, `xcover_version`, and a
      per-function list with `name`, `offset`, `hit` [A10].
- [ ] Tests: unknown cookie, sorted output, schema fields.

### PR 4: BPF sizing and hot path (`fix/bpf-map-sizing`)

- [ ] Size `seen_funcs` from the resolved function count before load, or
      fail init when the count exceeds the map [A6].
- [ ] Move the `seen_funcs` update after a successful ringbuf reserve [A6].
- [ ] Guard `bpf_printk` behind a debug build flag [A7].
- [ ] Attach in one `uprobe_multi` call or a large batch; fix the comment on
      the batch constant [A12].

### PR 5: `--pid` (`fix/pid-filter`)

- [ ] Plumb the flag through to the attach calls and record it in the report,
      or remove the flag and the README section [A8][B2].
- [ ] Note the 6.6 to 6.9 kernel thread-filter bug in docs [17].

### PR 6: preflight (`feat/preflight`)

- [ ] Detect `uprobe_multi` support at init and fail with a clear message
      naming the 6.6 minimum [A15][14].
- [ ] Check for `CAP_BPF` and `CAP_PERFMON` and report which is missing [23].

### PR 7: CI and tests (`test/e2e-ack`)

- [ ] e2e asserts `funcs_ack` contains the fixture functions [B9].
- [ ] Skip only on an explicit non-root check, not on error text [B9].
- [ ] Healthcheck tests use `t.TempDir()` sockets [A18].
- [ ] Benchmark driver waits on the health socket instead of sleeping [A18].

### PR 8: documentation truth (`docs/limitations`)

- [ ] Add a Limitations section: kernel 6.6+, `CAP_BPF` plus `CAP_PERFMON`,
      main executable only, non-inlined functions only, one name per aliased
      offset, per-hit trap cost with measured numbers [B4][B11][A15].
- [ ] Remove "Production-ready" and "traces all function calls" [B11].
- [ ] Add the Userspace BPF section to `README.md.tpl` [B10].
- [ ] Delete `docs/xcover_profile.md` and `docs/xcover_start.md` [B11].
- [ ] Fix `xcover_report.json`, `libbf-dev`, `make test` references [B11].
- [ ] Rewrite `demo/DEMO_SETUP.md` for the current layout [B12].
- [ ] State that entry-only probes avoid the Go uretprobe crash [25].

### Release hygiene

- [ ] Tag releases with a `v` prefix so pkg.go.dev resolves them [1].
- [ ] Per-arch BPF object output paths in `.goreleaser.yml` [B13].
- [ ] Document the runtime `libelf` and `zlib` dependency or link them
      statically [B13].
- [ ] Add version variables for the `-X` flags or drop the flags [B13].

## Phase 2: integrate with the coverage ecosystem

- [ ] LCOV exporter with `FN`, `FNDA`, `FNF`, `FNH` and synthesized `DA`
      lines for the function start line [48][49].
- [ ] Cobertura exporter for GitHub PR coverage [47].
- [ ] Land the merge command (PR #153) for multi-run aggregation.
- [ ] Function start lines from DWARF or `.gopclntab` for both exporters.

## Phase 3: runtime reachability

- [ ] `xcover reach`: take a list of vulnerable symbols (govulncheck, OSV),
      report hit or not-hit per symbol [37][38].
- [ ] Emit OpenVEX statements with `vulnerable_code_not_in_execute_path`
      for symbols never hit [42][43].
- [ ] Detach-on-first-hit mode for soak and production runs [51].
- [ ] Kubernetes DaemonSet mode: attach via `/proc/PID/root/<path>`, filter
      by mount namespace in BPF [53][54].

## Phase 4: dependency and platform risk

- [ ] Evaluate cilium/ebpf migration (no cgo, feature probes, BPF token) [5][6].
- [ ] Legacy per-function uprobe fallback for kernels 5.15 to 6.5.
- [ ] Keep bpftime strictly behind its build tag [7][8].

## Later

- [ ] RET-instruction uprobes for per-call duration [35].
- [ ] C++ and Rust demangling in reports.
- [ ] Shared-library coverage.
- [ ] Per-test attribution via an `xcover mark` command.
