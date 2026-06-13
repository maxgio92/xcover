#!/bin/bash
# Automated xcover debug file demo for asciinema
# Demonstrates coverage profiling on a stripped C binary using a separate
# debug file to resolve function names.
# Run as: sudo bash automated-demo-debugfile.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="./demo-app"
DEBUG_FILE="./demo-app.debug"
XCOVER="${SCRIPT_DIR}/../../xcover"

function cleanup() {
    ${XCOVER} stop 2>/dev/null || true
    pkill -f "${XCOVER} run" 2>/dev/null || true
    rm -f $DEMO_APP $DEBUG_FILE
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
	runCmd "# === xcover: Coverage with a separate debug file ==="
	runCmd "# Strip the binary for production. Keep the debug file for profiling."
	echo
	runCmd "# Let's test a demo C application"
	runCmd "bat ../src/demo-app.c"
	sleep 2
	clear
	runCmd "gcc -O0 -g -o demo-app ../src/demo-app.c"
	runCmd "# Extract debug info into a separate file"
	runCmd "objcopy --only-keep-debug demo-app demo-app.debug"
	runCmd "# Strip the production binary"
	runCmd "strip --strip-all demo-app"
	runCmd "readelf --symbols demo-app | wc -l"
	runCmd "# The debug file retains the symbols"
	runCmd "readelf --symbols demo-app.debug | wc -l"
	sleep 1
	clear
	runCmd "# Start the profiler — point it at both the binary and the debug file"
	runCmd "${XCOVER} run --detach --path demo-app --debug-path demo-app.debug --include '^(add|multiply|subtract|divide|greet)$'"
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
	runCmd "# Stripped binary, named coverage — debug file does the heavy lifting!"
}

function runCmd() {
	cmd=$1
	echo "$ ${cmd}"
	eval "${cmd}"
	sleep 2
}

main $@
