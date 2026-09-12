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
| linux-headers | BPF helper headers | matching `uname -r` |
| A kernel with BTF | `/sys/kernel/btf/vmlinux` must exist to generate `vmlinux.h` | GitHub `ubuntu-latest` |

On Debian or Ubuntu:

```shell
sudo apt-get install clang llvm gcc make pkg-config libelf-dev zlib1g-dev linux-headers-$(uname -r)
```

Most distributions ship a `bpftool` package. To build the same version CI uses:

```shell
git clone --branch v7.3.0 --recurse-submodules https://github.com/libbpf/bpftool.git
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
| End to end | see below | yes | Files tagged `e2e` under `e2e/`. Drives the real `./xcover` binary. |
| Benchmark | `make -C benchmark bench` | yes for kernel mode | See [benchmark/README.md](benchmark/README.md). |

Run the end-to-end tests the way CI does, so the test binary runs as root while
`go` stays your user's:

```shell
make xcover
go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
sudo rm -f /tmp/xcover.sock /tmp/xcover.pid /tmp/xcover.log
sudo env XCOVER_E2E_BIN="$PWD/xcover" /tmp/xcover-e2e.test -test.v
```

The e2e harness skips, rather than fails, when `XCOVER_E2E_BIN` is unset, when
stale `/tmp/xcover.*` files exist, or when BPF loading is denied. A green run
without root therefore proves little; check for `SKIP` lines.

Limit any Go target to a package with `TEST_PATH`, for example
`make test-integration TEST_PATH=./pkg/trace`.

## Lint

CI runs `gofmt -l .` and `go mod verify`. Run `gofmt -w .` before pushing.

## Documentation

`README.md` and `docs/xcover*.md` are generated:

- `docs/docs.go` renders one Markdown page per cobra command into `docs/`.
- It then substitutes the root command page into `README.md.tpl` at the
  `{{ .CLI_REFERENCE }}` marker and writes `README.md`.

Edit `README.md.tpl`, never `README.md`, and run `make docs` (or
`make xcover-container` followed by the same target inside the container).
Also run it after changing any command `Short`, `Long` or flag help text.
Hand-written pages such as `docs/xcover_userspace_bpf.md` and
`docs/architecture.md` are not touched by the generator.

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

## Reporting bugs and asking questions

Use [GitHub issues](https://github.com/maxgio92/xcover/issues). Include the
kernel version (`uname -r`), how xcover was built, the exact command line, the
contents of `/tmp/xcover.log` when using `--detach`, and whether the target
binary is stripped, static, or built with Go.
