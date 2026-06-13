#!/bin/bash
# Automated xcover stripped binary demo for asciinema
# Demonstrates coverage profiling on a stripped C binary via kernel uprobes.
# Run as: sudo bash automated-demo-stripped.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="./demo-app"
XCOVER="${SCRIPT_DIR}/../../xcover"

function cleanup() {
    ${XCOVER} stop 2>/dev/null || true
    pkill -f "${XCOVER} run" 2>/dev/null || true
    rm -f $DEMO_APP
    rm -f /tmp/xcover.*
}

trap cleanup EXIT

function main() {
	# Check if running as root
	if [ "$EUID" -ne 0 ]; then
	    echo "Please run as root: sudo bash $0"
	    exit 1
	fi

	clear
	runCmd "# === xcover: Coverage on stripped binaries ==="
	runCmd "# No source instrumentation. No debug info. Just the binary."
	echo
	runCmd "# Let's test a demo C application"
	runCmd "bat ../src/demo-app.c"
	sleep 2
	clear
	runCmd "gcc -O0 -o demo-app ../src/demo-app.c"
	runCmd "readelf --symbols demo-app | wc -l"
	runCmd "# Strip all symbols — this is what a production binary looks like"
	runCmd "strip --strip-all demo-app"
	runCmd "readelf --symbols demo-app | wc -l"
	runCmd "# Start the profiler before running the functional tests"
	runCmd "${XCOVER} run --detach --path demo-app --include '^(add|multiply|subtract|divide|greet)$'"
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
	runCmd "# Coverage profiled on a fully stripped binary — no debug info needed!"
}

function runCmd() {
	cmd=$1
	echo "$ ${cmd}"
	eval "${cmd}"
	sleep 2
}

main $@
