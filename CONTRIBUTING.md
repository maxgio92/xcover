# Contributing to xcover

Thanks for your interest. This page covers what you need to build, test and
change xcover. For how the code fits together read
[docs/architecture.md](docs/architecture.md).

## Prerequisites

xcover links libbpf statically from the `libbpfgo` git submodule and embeds a
BPF object compiled with clang. You need:

| Tool | Why | Version used in CI |
|---|---|---|
| Go | frontend | `go.mod` toolchain (1.26.x) |
| gcc, make, pkg-config | cgo and the libbpf build | Ubuntu default |
| clang, llvm | compile `bpf/trace.bpf.c` to BPF bytecode | Ubuntu default |
| bpftool | dump `bpf/vmlinux.h` from the running kernel's BTF | v7.3.0, built from source |
| libelf-dev, zlib1g-dev | link-time dependencies of libbpf | Ubuntu default |
| libbpf-dev | `bpf/bpf_helpers.h` and friends included by `bpf/trace.bpf.c` | Ubuntu default, then overridden (see below) |
| linux-headers | BPF helper headers | matching `uname -r` |
| A kernel with BTF | `/sys/kernel/btf/vmlinux` must exist to generate `vmlinux.h` | GitHub `ubuntu-latest` |

On Debian or Ubuntu:

```shell
sudo apt-get install clang llvm gcc make pkg-config libelf-dev zlib1g-dev libbpf-dev linux-headers-$(uname -r)
```

Most distributions ship a `bpftool` package. CI builds bpftool v7.3.0 from
source and, before that, installs the libbpf bundled in the same tree so the
BPF compile sees newer headers than the distribution's `libbpf-dev` (see
`.github/workflows/ci.yml`). Do the same when `bpf/bpf_helpers.h` is missing
or too old for `bpf/trace.bpf.c`:

```shell
git clone --branch v7.3.0 --recurse-submodules https://github.com/libbpf/bpftool.git
sudo make -C bpftool/libbpf/src install && sudo ldconfig
make -C bpftool/src && sudo make -C bpftool/src install
```

Extra prerequisites for the userspace BPF build (`make xcover-userspace`):
cmake 3.16+, a C++17 compiler and LLVM 18 (bpftime's JIT). The Makefile looks
for LLVM 18 through `brew --prefix llvm@18`; on Linux without Homebrew it falls
back to whatever cmake finds.

If you prefer not to install any of this, `make xcover-container` runs the
build inside the pinned `ghcr.io/maxgio92/xcover-build` image. It still needs
Docker and a BTF-enabled host kernel, because the container mounts
`/sys/kernel/btf` read-only.

## Get the source

```shell
git clone --recurse-submodules https://github.com/maxgio92/xcover.git
```

If you cloned without `--recurse-submodules`, run
`git submodule update --init --recursive`. The `libbpfgo` submodule commit must
match the `github.com/aquasecurity/libbpfgo` version in `go.mod`; CI enforces
this in the `submodule-sync` job. When Dependabot bumps libbpfgo in `go.mod`,
move the submodule to the same commit in the same PR.

## Build

| Target | What it does |
|---|---|
| `make xcover` | Builds libbpf from the submodule, compiles the BPF object, builds the `xcover` binary. Use this first. |
| `make xcover/bpf` | Compiles only `bpf/trace.bpf.c` into `pkg/probe/output/trace.bpf.o`. Generates `bpf/vmlinux.h` if missing. Pass `CFLAGS=-DDEBUG` to compile in the `bpf_printk` calls, readable from `/sys/kernel/debug/tracing/trace_pipe`. |
| `make xcover/frontend` | Builds only the Go binary. Needs the BPF object and libbpf already built, because the object is embedded with `go:embed`. |
| `make xcover-userspace` | Clones and builds pinned bpftime, applies `patches/bpftime/*.patch`, embeds the two shared libraries and builds `xcover-userspace` with `-tags userspace`. |
| `make bpftime-libs` | Only the bpftime step above. |
| `make xcover-container`, `make xcover-container-userspace` | Same as `make xcover` / `make xcover-userspace` inside the build image. |
| `make clean` | Removes build output and the bpftime checkout. It also removes the `libbpfgo` working tree, so run `git submodule update --init --recursive` afterwards. |

Plain `go build` does not work from a fresh checkout: it needs the CGO flags
set by the Makefile and the embedded BPF object.

## Test

| Layer | Command | Needs root | Notes |
|---|---|---|---|
| Unit | `make test` | no | `go test ./...` |
| Integration | `make test-integration` | no | Adds files tagged `integration` under `pkg/trace`. This is what CI runs. |
| End to end | `make test-e2e` | yes (prompts for sudo) | Files tagged `e2e` under `e2e/`. Drives the real `./xcover` binary, so run `make xcover` first. |
| Benchmark | `make -C benchmark bench` | yes for kernel mode | See [benchmark/README.md](benchmark/README.md). |

`make test-e2e` does what the CI `e2e` job does: it compiles the test binary
with `go test -c -tags e2e` as your user, removes stale `/tmp/xcover.*`
files, then runs the compiled binary under `sudo` with `XCOVER_E2E_BIN`
pointing at `./xcover`. Only the test binary runs as root; `go` and its cache
stay yours.

```shell
make xcover
make test-e2e
```

The e2e harness skips, rather than fails, when `XCOVER_E2E_BIN` is unset, when
stale `/tmp/xcover.*` files exist, or when BPF loading is denied. A green run
without root therefore proves little; check for `SKIP` lines.

Limit any Go target to a package with `TEST_PATH`, for example
`make test-integration TEST_PATH=./pkg/trace`.

## Lint

CI runs `gofmt -l .` and `go mod verify`. Run `gofmt -w .` before pushing.

## Documentation

`README.md` and every `docs/xcover*.md` page are generated:

- `docs/docs.go` renders one Markdown page per cobra command into `docs/`,
  named `xcover.md`, `xcover_run.md` and so on.
- It then substitutes the root command page into `README.md.tpl` at the
  `{{ .CLI_REFERENCE }}` marker and writes `README.md`.

Edit `README.md.tpl`, never `README.md`, and run `make docs` (or
`make xcover-container` followed by the same target inside the container).
Also run it after changing any command `Short`, `Long` or flag help text.
Every other page under `docs/` is hand-written and the generator does not
touch it. When you add a page, link it from [docs/README.md](docs/README.md),
which is the index by reader intent.

The generator does not delete pages for removed commands; delete them by hand.
The `build` CI job runs `make docs` and fails when `README.md` or `docs/`
differ from the committed files.

## Commit and pull request conventions

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat(trace): ...`, `fix(probe): ...`, `docs: ...`, `ci: ...`,
  `chore(deps): ...`. Scopes in use include `bpf`, `trace`, `probe`, `cmd`,
  `benchmark`, `demo`, `bpftime`, `makefile`, `release`.
- The release changelog groups `feat`, `fix`, `perf` and `refactor` and drops
  `docs`, `test`, `chore` and `ci`. A subject outside the convention lands in
  "Other changes".
- Open pull requests against `main`. Keep them focused; CI must pass
  (`build`, `submodule-sync`, `test`, `e2e`, `lint`).
- Changes to `bpf/trace.bpf.c` need a rebuilt BPF object; run at least
  `make xcover` and the e2e tests locally.

## Release

Releases are cut by pushing a tag matching `*.*.*` without a `v` prefix, for
example `0.6.0`. The `release` workflow cross-builds linux amd64 and arm64
archives with GoReleaser and publishes them with a `checksums.txt`. A PR that
touches only `.github/workflows/release.yml` or `.goreleaser.yml` runs a
snapshot build without publishing.

When tagging, check the release page. If archives now publish, update the
Install section of `README.md.tpl` and the status line of
`docs/userspace-bpf.md` so neither still tells readers to build from source
or wait for a release.

## Reporting bugs and asking questions

Use [GitHub issues](https://github.com/maxgio92/xcover/issues). Include the
kernel version (`uname -r`), how xcover was built, the exact command line, the
contents of `/tmp/xcover.log` when using `--detach`, and whether the target
binary is stripped, static, or built with Go.
