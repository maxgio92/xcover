#!/bin/bash
# log-pane.sh - tmux log pane helper for xcover demos.
#
# Usage:
#   source "$(dirname "${BASH_SOURCE[0]}")/../lib/log-pane.sh"
#
#   setup_log_pane kernel      # tail the xcover daemon log (/tmp/xcover.log)
#   setup_log_pane userspace   # tail the bpftime runtime log ($HOME/.bpftime/runtime.log)
#
#   Call teardown_log_pane in your cleanup() function.
#
# Both functions are no-ops outside tmux. sudo drops TMUX from the
# environment by default, so the kernel demos must run as
# `sudo --preserve-env=TMUX bash demo.sh` (or inside a tmux server started as
# root) for the pane to open.
#
# Kernel mode tails /tmp/xcover.log by default, the file `xcover run --detach`
# writes to (settings.LogFile). Set XCOVER_DEMO_TRACE_PIPE=1 to tail the kernel
# trace pipe instead, which shows bpf_printk output. That output is only
# present when xcover was built with `make xcover/bpf BPF_DEBUG=1` and then
# rebuilt. The pane command is forked by the tmux server, not by this (root)
# process, so the trace pipe is only readable when the tmux server itself runs
# as root; the pane script therefore picks its source with its own
# credentials. tracefs (/sys/kernel/tracing) is preferred, with a fallback to
# debugfs (/sys/kernel/debug/tracing). If neither is readable the pane falls
# back to the daemon log and says so.

# Preserve an open pane if this file is sourced twice.
LOG_PANE="${LOG_PANE:-}"

function setup_log_pane() {
    [ -z "${TMUX:-}" ] && return 0
    local mode="${1:-kernel}"
    local log_file="/tmp/xcover.log"
    local pane_cmd

    if [ "${mode}" = "userspace" ]; then
        log_file="${HOME}/.bpftime/runtime.log"
        # Optional pane: never let a read-only HOME abort the demo.
        mkdir -p "$(dirname "${log_file}")" 2>/dev/null || true
    fi

    # Create the file up front so tail -F has something to open; -F keeps
    # following when the demo cleanup removes and xcover recreates it.
    touch "${log_file}" 2>/dev/null || true
    # The readability test must run inside the pane: tmux forks the pane
    # command with the server's credentials, not this script's. $1 is the log
    # file, passed as a positional argument and expanded by the pane's bash.
    # shellcheck disable=SC2016
    if [ "${mode}" = "kernel" ] && [ "${XCOVER_DEMO_TRACE_PIPE:-0}" = "1" ]; then
        pane_cmd='for p in /sys/kernel/tracing/trace_pipe /sys/kernel/debug/tracing/trace_pipe; do [ -r "$p" ] && exec cat "$p"; done; echo "trace_pipe not readable, falling back to $1"; exec tail -n +1 -F "$1"'
    else
        pane_cmd='exec tail -n +1 -F "$1"'
    fi

    # -l 40% rather than -p 40: tmux 3.4 (Ubuntu 24.04) mis-parses -p and
    # fails with "size missing"; -l with a percentage works on 3.1 and later.
    # -d keeps focus on the demo pane, so no select-pane is needed.
    if ! LOG_PANE="$(tmux split-window -d -h -l 40% -P -F '#{pane_id}' bash -c "${pane_cmd}" _ "${log_file}")"; then
        LOG_PANE=""
        echo "log pane: tmux split-window failed, continuing without pane" >&2
    fi
}

function teardown_log_pane() {
    if [ -n "${LOG_PANE:-}" ]; then
        tmux kill-pane -t "${LOG_PANE}" 2>/dev/null || true
    fi
    LOG_PANE=""
}
