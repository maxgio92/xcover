# xcover documentation

Pages grouped by what you want to do. Start with the
[project README](../README.md) if you have not used xcover yet.

## Getting started

- [README](../README.md): requirements, install, quickstart and how it works.
- [Use in CI](../README.md#use-in-ci): a GitHub Actions job that fails a
  build on low coverage.

## Guides

- [Troubleshooting](troubleshooting.md): every user-facing message, its cause
  and its fix.
- [Userspace BPF mode](userspace-bpf.md): run without `CAP_BPF` through
  bpftime; experimental.
- [Benchmark](../benchmark/README.md): measure the per-call overhead on your
  machine.
- [Demos](../demo/README.md): scripted terminal sessions for each scenario.

## Reference

Generated from the command help by `make docs`. Do not edit by hand.

- [xcover](xcover.md): global flags and subcommand list.
- [xcover run](xcover_run.md)
- [xcover wait](xcover_wait.md)
- [xcover status](xcover_status.md)
- [xcover stop](xcover_stop.md)

## Design

- [Architecture](architecture.md): the pipeline from ELF to report, for
  contributors.
- [bpftime patches](../patches/bpftime/README.md): the fixes applied to the
  pinned bpftime checkout.

## Maintainer records

- [talks/](talks/): conference slides and speaker notes.

When you add a page, link it here and, if users need it, from the README.
