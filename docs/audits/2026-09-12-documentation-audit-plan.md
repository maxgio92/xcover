# Plan: follow-ups from the documentation audit

Source: [2026-09-12-documentation-audit.md](2026-09-12-documentation-audit.md).
Items are ordered by value over effort. Each item is one PR-sized change.

## Small fixes (this branch)

Items marked **Done** landed on `xcover-docs`. The userspace (bpftime)
flavour of items 1 and 6 is unverified: no CI job builds `-tags userspace`.

1. **Done.** **Wire `--pid`.** Pass `Options.pid` through the tracer to
   `AttachUprobeMulti` and `AttachUprobeWithOpts` instead of the constant -1.
   Update `docs/xcover_run.md` help text and drop the "not applied yet" note
   from `README.md.tpl`.
2. **Done.** **Return attach errors.** `Probe.Attach` must return the `uprobe_multi`
   error instead of `nil`, so `attachProbe` aborts and `run` exits non-zero.
3. **Done.** **Do not truncate `funcs_ack`.** In `writeReport`, skip unknown cookies
   with `return true` and compute `cov_by_func` from `len(ack)`.
4. **Done.** **Compile filters once.** Compile include and exclude regexes when the
   resolver is built and return an error for an invalid pattern.
5. **Done.** **Guard `bpf_printk`.** Compile the three `bpf_printk` calls out unless a
   `DEBUG` macro is set; keep `-DDEBUG` reachable through `CFLAGS`.
6. **Done.** **Check `bpf_map_update_elem`.** On failure, return without submitting an
   event and count drops in a small counter map or log once from userspace.
7. **Done.** **Propagate report file errors.** Return the `os.Create` error from
   `writeReport`.
8. **Done.** **Docs drift check in CI.** Add `make docs && git diff --exit-code` to the
   `build` job.
9. **Done.** **Fix arm64 CFLAGS.** Map `aarch64` to `arm64` for `-D__TARGET_ARCH_`.

## Documentation follow-ups

Second pass on the same branch, after the README rewrite.

- **Done.** `docs/troubleshooting.md` with one entry per user-facing message;
  the three most common inline in the README.
- **Done.** `libbpf-dev` and the CI libbpf header install in CONTRIBUTING.
- **Done.** Benchmark report paths fixed in `benchmark/README.md`.
- **Done.** `make test-e2e` mirrors the CI job; README, CONTRIBUTING and
  `e2e/doc.go` point at it.
- **Done.** `run --detach` "already running" behaviour documented.
- **Done.** "Use in CI" section in `README.md.tpl`.
- **Done.** `--verbose`, `--status` and `wait --timeout` help strings fixed;
  "Output and logging" paragraph added.
- **Done.** Project scope fallback described as a logged warning.
- **Done.** Overhead footnote with source and reproduction steps.
- **Done.** Trust note after Requirements.
- **Done.** `docs/xcover_userspace_bpf.md` renamed to `docs/userspace-bpf.md`
  and trimmed to user-facing content, with the bpftime `--pid` and map-full
  limitations.
- **Done.** Stale build recipe in `pkg/bpftime/bpftime.go` replaced.
- **Done.** `docs/architecture.md` ring buffer poll wording.
- **Done.** Install and status wording no longer tied to the current release.
- **Done.** `demo/DEMO_SETUP.md` renamed to `demo/README.md`.
- **Done.** `docs/README.md` index by reader intent, linked from the README
  and CONTRIBUTING.
- **Done.** CONTRIBUTING "generated pages" statement made exact.

## Medium

10. **Fix the release workflow** so tags publish archives; then update the
    Install section in `README.md.tpl`.
11. **Add `xcover version`** (issue #9) with the ldflags already set by
    GoReleaser.
12. **Record effective scope in the report** (issue #116) and warn on stderr
    even in `--detach` mode.
13. **Add a CI lint job** with `go vet` and `staticcheck`; trim the eBPF
    toolchain install from the lint job.
14. **Add `CODE_OF_CONDUCT.md`, `SECURITY.md` and issue templates.**

## Larger

15. **Report merge** across runs (draft PR #153).
16. **Inlined functions** (issue #51): evaluate DWARF inline-site probing.
17. **Non-Go project scope** (issue #100): DWARF `DW_AT_decl_file` with a
    `--source-root`.
18. **Configurable state paths** (`--pid-file`, `--log-file`, `--socket-path`
    on `run`) to allow more than one daemon per host.
