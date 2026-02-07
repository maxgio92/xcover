#!/bin/bash
# Generate a simulated xcover demo cast file
# This creates a realistic asciinema recording without requiring actual sudo

OUTPUT_FILE="${1:-/tmp/xcover-demo.cast}"

# Cast file header
cat > "$OUTPUT_FILE" << 'EOF_HEADER'
{"version": 2, "width": 100, "height": 30, "timestamp": 1770504000, "env": {"SHELL": "/bin/bash", "TERM": "xterm-256color"}}
EOF_HEADER

# Generate cast events with realistic timing
cat >> "$OUTPUT_FILE" << 'EOF_EVENTS'
[0.5, "o", "\u001b[H\u001b[2J"]
[0.7, "o", "=== xcover: Functional Test Coverage Profiler ===\r\n"]
[0.8, "o", "\r\n"]
[1.5, "o", "# Profile coverage without instrumenting your binaries!\r\n"]
[1.7, "o", "# Uses eBPF to trace function calls at runtime\r\n"]
[1.8, "o", "\r\n"]
[3.0, "o", "\u001b[32m$\u001b[0m xcover run --detach --path demo-app\r\n"]
[3.5, "o", "Starting xcover profiler in daemon mode...\r\n"]
[4.0, "o", "Profiler started successfully (PID: 12345)\r\n"]
[4.2, "o", "Tracing binary: demo-app\r\n"]
[5.5, "o", "\r\n"]
[5.7, "o", "\u001b[32m$\u001b[0m xcover wait  # Wait for profiler to be ready\r\n"]
[6.0, "o", "Waiting for profiler to be ready...\r\n"]
[7.0, "o", "Profiler is ready!\r\n"]
[8.0, "o", "\r\n"]
[8.2, "o", "# Run test scenarios - xcover is tracing all function calls\r\n"]
[8.5, "o", "\u001b[32m$\u001b[0m ./demo-app add\r\n"]
[9.0, "o", "Testing add function: 5 + 3 = 8\r\n"]
[10.0, "o", "\u001b[32m$\u001b[0m ./demo-app multiply\r\n"]
[10.5, "o", "Testing multiply function: 4 * 7 = 28\r\n"]
[11.5, "o", "\u001b[32m$\u001b[0m ./demo-app greet\r\n"]
[12.0, "o", "Hello from xcover demo!\r\n"]
[13.0, "o", "\r\n"]
[13.2, "o", "\u001b[32m$\u001b[0m xcover stop  # Stop profiler and generate report\r\n"]
[13.5, "o", "Stopping profiler...\r\n"]
[14.5, "o", "Profiler stopped. Report generated: xcover-report.json\r\n"]
[15.5, "o", "\r\n"]
[15.7, "o", "# View coverage results:\r\n"]
[15.9, "o", "\u001b[32m$\u001b[0m cat xcover-report.json | jq '.cov_by_func'\r\n"]
[16.5, "o", "{\r\n"]
[16.6, "o", "  \"main.add\": true,\r\n"]
[16.7, "o", "  \"main.divide\": false,\r\n"]
[16.8, "o", "  \"main.greet\": true,\r\n"]
[16.9, "o", "  \"main.main\": true,\r\n"]
[17.0, "o", "  \"main.multiply\": true,\r\n"]
[17.1, "o", "  \"main.subtract\": false\r\n"]
[17.2, "o", "}\r\n"]
[18.5, "o", "\u001b[32m$\u001b[0m cat xcover-report.json | jq '.funcs_traced | length'\r\n"]
[19.0, "o", "\u001b[33m4\u001b[0m\r\n"]
[20.0, "o", "\r\n"]
[20.2, "o", "\u001b[32m✅ Coverage profiled without source code or recompilation!\u001b[0m\r\n"]
[21.5, "o", "\r\n"]
EOF_EVENTS

echo "Created simulated demo cast file: $OUTPUT_FILE"
