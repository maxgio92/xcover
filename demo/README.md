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
sudo --preserve-env=TMUX bash demo.sh   # userspace scenarios run without sudo
```

## Log pane

When a demo runs inside tmux it splits a pane on the right that tails
`/tmp/xcover.log`, the file `xcover run --detach` writes to (the userspace
scenarios tail `~/.bpftime/runtime.log` instead). sudo drops `TMUX` from the
environment by default, so the kernel demos need `sudo --preserve-env=TMUX`
(or a tmux server started as root) for the pane to open; with plain `sudo` the
demo runs without a pane. On a BPF_DEBUG build (`make xcover/bpf BPF_DEBUG=1`,
then rebuild xcover) set `XCOVER_DEMO_TRACE_PIPE=1` to tail the kernel trace
pipe and see `bpf_printk` output. The trace pipe is root-only and the pane is
spawned by the tmux server, so this only works when tmux itself runs as root;
otherwise the pane prints a notice and falls back to the log file. The pane is
closed when the demo exits. Outside tmux nothing changes.

## Record and publish

```shell
cd demo/basic
asciinema rec -t "xcover - Functional Test Coverage Profiler" \
  --command "sudo --preserve-env=TMUX ./demo.sh" xcover-demo.cast
asciinema upload xcover-demo.cast
```

To change the recording embedded in the README, edit the asciicast link in
`README.md.tpl` (not `README.md`, which is generated) and run `make docs`.
