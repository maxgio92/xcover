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
| libbpf-dev | `bpf/bpf_helpers.h` and `bpf/bpf_core_read.h`, included by `bpf/trace.bpf.c` | Ubuntu default, then overridden (see below) |
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
| `make xcover/bpf` | Compiles only `bpf/trace.bpf.c` into `pkg/probe/output/trace.bpf.o`. Generates `bpf/vmlinux.h` if missing. |
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
| End to end | `make test-e2e` or see below | yes | Files tagged `e2e` under `e2e/`. Drives the real `./xcover` binary, so run `make xcover GO_BUILD_FLAGS='-tags e2etest'` first. The C++ scenario needs `g++` and skips without it, or fails when `XCOVER_E2E_REQUIRE=1` is set. |
| Benchmark | `make -C benchmark bench` | yes for kernel mode | See [benchmark/README.md](benchmark/README.md). |

`make test-e2e` runs `go test -count=1 -tags e2e ./e2e` as the current user
with `XCOVER_E2E_BIN` pointing at `./xcover`. It does not escalate
privileges. As a normal user the harness notices it is not root and skips
before it starts xcover, or fails when `XCOVER_E2E_REQUIRE=1` is set.
Either run the whole target as root, or do what the CI `e2e` job does:
compile the test binary as your user and run only that binary under `sudo`,
so `go` and its cache stay yours:

```shell
make xcover GO_BUILD_FLAGS='-tags e2etest'
go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
sudo rm -f /tmp/xcover.sock /tmp/xcover.pid /tmp/xcover.log
sudo env XCOVER_E2E_BIN="$PWD/xcover" /tmp/xcover-e2e.test -test.v -test.count=1
```

The e2e harness skips when `XCOVER_E2E_BIN` is unset, when stale
`/tmp/xcover.*` files exist, or when it is not running as root. Set
`XCOVER_E2E_REQUIRE=1` to turn those skips into failures, as CI does:

```shell
sudo env XCOVER_E2E_BIN="$PWD/xcover" XCOVER_E2E_REQUIRE=1 /tmp/xcover-e2e.test -test.v
```

Any xcover error past the preconditions, including a denied BPF load, fails
the test. A green run without root and without the variable proves little;
check for `SKIP` lines.

The recipes above build xcover with `-tags e2etest`, as CI does: such a binary
honours `XCOVER_E2E_SEEN_FUNCS_MAX` to cap the `seen_funcs` map so the e2e
suite can provoke the drops warning, while release builds ignore the variable.

The e2e tests build their Go fixture programs from `e2e/testdata` with
`go build` at run time. `make e2e-fixtures` builds them ahead of time, one
binary per module, into `E2E_FIXTURES_DIR` (default `/tmp/xcover-e2e-fixtures`).
Point `XCOVER_E2E_FIXTURES` at that directory and the harness uses the
prebuilt binaries instead. The C++ fixture is not covered; it compiles in the
test.

`e2e/doc.go` holds a feature matrix that maps every `run` flag, command and
failure path to the scenario that covers it, or states why none does. When
you add a flag or a command, add a scenario and a row.

Limit any Go target to a package with `TEST_PATH`, for example
`make test-integration TEST_PATH=./pkg/trace`.

CI also collects coverage from the unit tests and from the e2e-driven
`./xcover` binary, then merges both into one `coverage.txt` artifact. To
reproduce it locally, build the binary with `-cover`, point each run at its
own `GOCOVERDIR`, and merge the two directories with `go tool covdata`:

```shell
rm -rf /tmp/cov-unit /tmp/cov-e2e && mkdir -p /tmp/cov-unit /tmp/cov-e2e
make xcover GO_BUILD_FLAGS='-cover -tags e2etest'
make test-integration GO_TEST_FLAGS="-cover -args -test.gocoverdir=/tmp/cov-unit"
go test -c -tags e2e -o /tmp/xcover-e2e.test ./e2e
sudo rm -f /tmp/xcover.sock /tmp/xcover.pid /tmp/xcover.log
sudo env XCOVER_E2E_BIN="$PWD/xcover" XCOVER_E2E_REQUIRE=1 GOCOVERDIR=/tmp/cov-e2e /tmp/xcover-e2e.test -test.v -test.count=1
sudo chown -R "$(id -u):$(id -g)" /tmp/cov-e2e
go tool covdata textfmt -i=/tmp/cov-unit,/tmp/cov-e2e -o coverage.txt
go tool cover -func coverage.txt
```

`GOCOVERDIR` must go inside `sudo env`, because sudo resets the environment
otherwise. The binary writes its counters as root, so chown the directory
before reading it.

### Kernel matrix

The `e2e` job runs on the `ubuntu-24.04` runner kernel and logs `uname -r`.
The `e2e-kernel` job runs the same suite on Linux 6.6 and 6.12 inside a QEMU
VM booted by [vimto](https://github.com/lmb/vimto), with the kernel images
from `ghcr.io/cilium/ci-kernels`. `.vimto.toml` sets the image, root user,
memory and CPUs; the job overrides only the tag with `-kernel :TAG`. The VM
shares the host root filesystem, so the binaries built on the host run inside
it, but `/tmp` is empty, so the job builds the test binary and the fixtures
under the checkout. To reproduce one kernel locally (needs `/dev/kvm` and
`qemu-system-x86`):

```shell
make xcover GO_BUILD_FLAGS='-tags e2etest'
go test -c -tags e2e -o "$PWD/xcover-e2e.test" ./e2e
make e2e-fixtures E2E_FIXTURES_DIR="$PWD/xcover-e2e-fixtures"
CGO_ENABLED=0 go install lmb.io/vimto@v0.4.0
vimto -kernel :6.6 exec -- sh -c 'XCOVER_E2E_BIN=$PWD/xcover XCOVER_E2E_REQUIRE=1 XCOVER_E2E_FIXTURES=$PWD/xcover-e2e-fixtures $PWD/xcover-e2e.test -test.v -test.count=1' </dev/null
```

`vimto exec` refuses a cgo build, hence `CGO_ENABLED=0`. Redirect stdin from
`/dev/null`: with a closed pipe on stdin QEMU marks the guest console
disconnected and the first write from the guest blocks. The `--pid`
thread-filter warning scenario skips with a plain `t.Skipf` on kernels that
carry the upstream fix (6.6.35, 6.9.5, 6.10 or newer), so a `SKIP` line for
`TestPIDFilterWarnsOnThreadFilteringKernel` is expected there even with
`XCOVER_E2E_REQUIRE=1`.

## Lint

CI runs `gofmt -l .`, `go mod verify`, and `go vet` for the default, `e2e`,
`e2etest`, `integration` and `docs` build tags. Run `gofmt -w .` before pushing.

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
which is the index by reader intent. The e2e feature matrix lives in
`e2e/doc.go`, not under `docs/`; a new `run` flag or command gets a row there
next to its scenario.

The generator does not delete pages for removed commands; delete them by hand.
CI runs `make docs` and fails when the generated files differ from the commit.

## Commit and pull request conventions

- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat(trace): ...`, `fix(probe): ...`, `docs: ...`, `ci: ...`,
  `chore(deps): ...`. Scopes in use include `bpf`, `trace`, `probe`, `cmd`,
  `benchmark`, `demo`, `bpftime`, `makefile`, `release`.
- The release changelog groups `feat`, `fix` and `perf` and drops `docs`,
  `test`, `chore`, `ci`, `build` and `refactor`. The release notes show those
  groups plus "Other Changes" for subjects outside the convention, and link to
  the full changelog.
- Open pull requests against `main`. Keep them focused; CI must pass
  (`build`, `submodule-sync`, `test`, `e2e`, `e2e-kernel`, `coverage`,
  `lint`).
- Changes to `bpf/trace.bpf.c` need a rebuilt BPF object; run at least
  `make xcover` and the e2e tests locally.

## Release

Releases are cut by pushing a tag matching `*.*.*` without a `v` prefix, for
example `0.6.0`. The `release` workflow cross-builds linux amd64 and arm64
archives with GoReleaser and publishes them with a `checksums.txt`. A PR that
touches only `.github/workflows/release.yml` or `.goreleaser.yml` runs a
snapshot build without publishing.

When tagging, check the release page. Confirm the README Install section and
any experimental status note in `docs/` match the published assets.

## Reporting bugs and asking questions

Use [GitHub issues](https://github.com/maxgio92/xcover/issues). Include the
kernel version (`uname -r`), how xcover was built, the exact command line, the
contents of `/tmp/xcover.log` when using `--detach`, and whether the target
binary is stripped, static, or built with Go.
