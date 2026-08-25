#!/bin/bash
# Automated xcover userspace BPF demo for asciinema
# Requires bpftime libs to be built: make bpftime-libs && go build .
# Run as: sudo bash automated-demo-userspace.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="./demo-app"
XCOVER="${SCRIPT_DIR}/../../xcover-userspace"
SLEEP="${SLEEP:-2}"

function cleanup() {
    ${XCOVER} stop 2>/dev/null || true
    pkill -f "${XCOVER} run" 2>/dev/null || true
    rm -f /dev/shm/bpftime_*
    if [ -n "${XCOVER_AGENT:-}" ] && [ -f "${XCOVER_AGENT}" ]; then
        rm -f "${XCOVER_AGENT}"
    fi
    rm -f $DEMO_APP
    rm -f /tmp/xcover.*
}

trap cleanup EXIT

function main() {
	clear
	runCmd "# === xcover: Userspace BPF mode (powered by bpftime) ==="
	runCmd "# Same coverage profiling. Zero kernel traps!"
	echo
	runCmd "# Let's test a demo C application"
	runCmd "bat ../src/c/demo-app.c"
	sleep "${SLEEP}"
	clear
	runCmd "gcc -O0 -o demo-app ../src/c/demo-app.c"
	runCmd "ls demo-app"
	runCmd "# In userspace BPF mode the tracee must be dynamically linked"
	runCmd "ldd demo-app"
	sleep 1
	clear
	runCmd "# Extract the bpftime agent library"
	runCmd "export XCOVER_AGENT=\$(${XCOVER} agent extract)"
	runCmd "echo \$XCOVER_AGENT"
	sleep 1
	export BPFTIME_SHM_MEMORY_MB=2048
	runCmd "${XCOVER} run --detach --path demo-app --include '^(main|add|multiply|subtract|divide|greet)$' --userspace-bpf"
	runCmd "# Wait for the profiler to be ready"
	runCmd "${XCOVER} wait"
	sleep 1
	clear
	runCmd "# Run test scenarios with the agent preloaded"
	runCmd "LD_PRELOAD=\$XCOVER_AGENT ./demo-app add"
	runCmd "LD_PRELOAD=\$XCOVER_AGENT ./demo-app multiply"
	runCmd "LD_PRELOAD=\$XCOVER_AGENT ./demo-app greet"
	clear
	runCmd "# Stop and collect results"
	runCmd "${XCOVER} stop"
	runCmd "cat xcover-report.json | jq '.cov_by_func'"
	runCmd "cat xcover-report.json | jq '.funcs_traced | length'"
	runCmd "cat xcover-report.json | jq '.funcs_ack | length'"
	runCmd "cat xcover-report.json | jq"
	runCmd "# Coverage profiled entirely in userspace — no kernel traps, no instrumentation!"
}

function runCmd() {
	cmd=$1
	echo "$ ${cmd}"
	eval "${cmd}"
	sleep "${SLEEP}"
}

main $@
