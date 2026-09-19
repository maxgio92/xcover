#!/bin/bash
# Automated xcover demo for asciinema
# Run as: sudo --preserve-env=TMUX,TMUX_PANE bash demo.sh   (TMUX and TMUX_PANE are needed for the log pane)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="./demo-app"
XCOVER="${SCRIPT_DIR}/../../xcover"
SLEEP="${SLEEP:-2}"
# Show sources with bat when installed, plain cat otherwise.
# Debian and Ubuntu install bat as batcat.
SRC_VIEWER="cat"
if command -v bat >/dev/null 2>&1; then
    SRC_VIEWER="bat --paging=never"
elif command -v batcat >/dev/null 2>&1; then
    SRC_VIEWER="batcat --paging=never"
fi

# shellcheck source=../lib/log-pane.sh
source "${SCRIPT_DIR}/../lib/log-pane.sh"

function cleanup() {
    teardown_log_pane
    ${XCOVER} stop 2>/dev/null || true
    pkill -f "${XCOVER} run" 2>/dev/null || true
    rm -f $DEMO_APP
    rm -f /tmp/xcover.*
}

trap cleanup EXIT

function main() {
	# Check if running as root
	if [ "$EUID" -ne 0 ]; then
	    echo "Please run as root: sudo --preserve-env=TMUX,TMUX_PANE bash $0"
	    exit 1
	fi

	setup_log_pane kernel
	clear
	runCmd "# === xcover: Functional Test Coverage Profiler ==="
	runCmd "# Profile coverage without instrumenting your binaries!"
	echo
	runCmd "# Let's test a demo Go application"
	runCmd "${SRC_VIEWER} ../src/go/demo-app.go"
	sleep "${SLEEP}"
	clear
	runCmd "go build -o demo-app ../src/go/"
	runCmd "ls demo-app"
	runCmd "# Start the profiler before running the functional tests"
	runCmd "${XCOVER} run --detach --path demo-app --scope project"
	runCmd "# Wait for the profiler to be ready"
	runCmd "${XCOVER} wait"
	runCmd "# Run test scenarios - xcover is tracing all function calls"
	clear
	runCmd "./demo-app add"
	runCmd "./demo-app multiply"
	runCmd "./demo-app greet"
	runCmd "# Now let's stop the profiler"
	clear
	runCmd "${XCOVER} stop"
	runCmd "# Collect the coverage results:"
	runCmd "cat xcover-report.json | jq '.cov_by_func'"
	runCmd "cat xcover-report.json | jq '.funcs_traced | length'"
	runCmd "cat xcover-report.json | jq '.funcs_ack | length'"
	runCmd "cat xcover-report.json | jq"
	runCmd "# Coverage profiled without source code changes or recompilation!"
}

function runCmd() {
	cmd=$1
	echo "$ ${cmd}"
	eval "${cmd}"
	sleep "${SLEEP}"
}

main "$@"
