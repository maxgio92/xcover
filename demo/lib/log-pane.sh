#!/bin/bash
# log-pane.sh — tmux log pane helper for xcover demos.
#
# Usage:
#   source "$(dirname "${BASH_SOURCE[0]}")/../lib/log-pane.sh"
#
#   setup_log_pane kernel      # tail /sys/kernel/debug/tracing/trace_pipe
#   setup_log_pane userspace   # tail ~/.bpftime/runtime.log
#
#   Call teardown_log_pane in your cleanup() function.

LOG_PANE=""

function setup_log_pane() {
    [ -z "${TMUX:-}" ] && return
    local mode="${1:-kernel}"
    local log_cmd
    if [ "$mode" = "userspace" ]; then
        log_cmd="tail -f ~/.bpftime/runtime.log"
    else
        log_cmd="cat /sys/kernel/debug/tracing/trace_pipe"
    fi
    LOG_PANE=$(tmux split-window -h -P -F "#{pane_id}" "$log_cmd")
    tmux select-pane -L
}

function teardown_log_pane() {
    [ -n "${LOG_PANE:-}" ] && tmux kill-pane -t "$LOG_PANE" 2>/dev/null || true
    LOG_PANE=""
}
