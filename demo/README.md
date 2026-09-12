# xcover demos

Scripted terminal sessions for recording with asciinema. Each scenario is a
`demo.sh` in its own directory and cleans up after itself.

| Directory | What it shows | Binary | Needs |
|---|---|---|---|
| `basic/` | Kernel mode on a Go program with `--scope project` | `../../xcover` | root |
| `stripped/` | Kernel mode on a stripped C program (function recovery) | `../../xcover` | root |
| `debugfile/` | Kernel mode on a stripped C program with `--debug-path` | `../../xcover` | root, `objcopy`, `strip`, `readelf` |
| `userspace/` | Userspace BPF mode on a C program with `LD_PRELOAD` | `../../xcover-userspace` | no root |
| `userspace-stripped/` | Userspace BPF mode on a stripped C program | `../../xcover-userspace` | no root |

Shared sources live in `src/go/demo-app.go` and `src/c/demo-app.c`. All scripts
also call `bat`, `jq`, `gcc` or `go`, and honour `SLEEP` (seconds between
commands, default 2).

## Run a demo

Build the binary the scenario needs from the repository root, then run the
script from its directory:

```shell
make xcover                    # or: make xcover-userspace
cd demo/basic
sudo bash demo.sh              # userspace scenarios run without sudo
```

## Record and publish

```shell
cd demo/basic
asciinema rec -t "xcover - Functional Test Coverage Profiler" \
  --command "sudo ./demo.sh" xcover-demo.cast
asciinema upload xcover-demo.cast
```

To change the recording embedded in the README, edit the asciicast link in
`README.md.tpl` (not `README.md`, which is generated) and run `make docs`.
