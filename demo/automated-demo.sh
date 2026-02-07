#!/bin/bash
# Automated xcover demo for asciinema
# Run as: sudo bash automated-demo.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_APP="$SCRIPT_DIR/demo-app"
XCOVER="$(cd "$SCRIPT_DIR/.." && pwd)/xcover"

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root: sudo bash $0"
    exit 1
fi

clear
echo "=== xcover: Functional Test Coverage Profiler ==="
echo
sleep 1

echo "# Profile coverage without instrumenting your binaries!"
echo "# Uses eBPF to trace function calls at runtime"
echo
sleep 2

echo "$ xcover run --detach --path demo-app"
$XCOVER run --detach --path $DEMO_APP
sleep 2

echo
echo "$ xcover wait  # Wait for profiler to be ready"
$XCOVER wait
sleep 1

echo
echo "# Run test scenarios - xcover is tracing all function calls"
echo "$ ./demo-app add"
sleep 1
$DEMO_APP add
sleep 1

echo "$ ./demo-app multiply"
sleep 1
$DEMO_APP multiply
sleep 1

echo "$ ./demo-app greet"
sleep 1
$DEMO_APP greet
sleep 1

echo
echo "$ xcover stop  # Stop profiler and generate report"
$XCOVER stop
sleep 2

echo
echo "# View coverage results:"
echo "$ cat xcover-report.json | jq '.cov_by_func'"
cat xcover-report.json | jq '.cov_by_func'
sleep 2

echo
echo "$ cat xcover-report.json | jq '.funcs_traced | length'"
cat xcover-report.json | jq '.funcs_traced | length'
sleep 2

echo
echo "✅ Coverage profiled without source code or recompilation!"
sleep 2
