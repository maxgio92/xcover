#!/bin/bash
# Automated xcover demo for asciinema
# Run as: sudo bash automated-demo.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="./demo-app"
XCOVER="xcover"
PATH="/home/linuxbrew/.linuxbrew/bin:${PATH}"
export $PATH

function main() {
	# Check if running as root
	if [ "$EUID" -ne 0 ]; then
	    echo "Please run as root: sudo bash $0"
	    exit 1
	fi

	clear
	runCmd "# === xcover: Functional Test Coverage Profiler ==="
	runCmd "# Profile coverage without instrumenting your binaries!"
	echo
	runCmd "# Let's test a demo Go application"
	runCmd "bat demo-app.go"
	sleep 2
	clear
	runCmd "go build demo-app.go"
	runCmd "ls demo-app"
	runCmd "readelf --symbols demo-app | wc -l"
	runCmd "# Let's strip the binary, because this must be a production binary"
	runCmd "strip --strip-all demo-app"
	runCmd "readelf --symbols demo-app | wc -l"
	runCmd "# Start the profiler before running the functional tests"
	runCmd "xcover run --detach --path demo-app --include '^main\.'"
	runCmd "# Wait for the profiler to be ready"
	runCmd "xcover wait"
	runCmd "# Run test scenarios - xcover is tracing all function calls"
	clear
	runCmd "./demo-app add"
	runCmd "./demo-app multiply"
	runCmd "./demo-app greet"
	runCmd "# Now let's stop the profiler"
	clear
	runCmd "xcover stop"
	runCmd "# Collect the coverage results:"
	runCmd "cat xcover-report.json | jq '.cov_by_func'"
	runCmd "cat xcover-report.json | jq '.funcs_traced | length'"
	runCmd "cat xcover-report.json | jq '.funcs_ack | length'"
	runCmd "cat xcover-report.json | jq"
	runCmd "# Coverage profiled without source code changes or recompilation on production binaries!"
}

function runCmd() {
	cmd=$1
	echo "$ ${cmd}"
	eval "${cmd}"
	sleep 2
}

main $@
