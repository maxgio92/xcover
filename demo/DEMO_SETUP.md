# xcover Asciinema Demo Setup

## Overview

This directory contains everything needed to create an impressive asciinema demo of xcover.

## Files

- `demo-app.go` - Simple Go application with multiple functions for demonstration
- `demo-app` - Compiled binary (used as the target for coverage profiling)
- `automated-demo.sh` - Complete demo script (run with sudo)
- `RECORDING_INSTRUCTIONS.md` - Detailed recording instructions

## Quick Start - Record the Demo

The fastest way to record the demo:

```bash
# From the repository root, go to demo directory
cd demo

# Record the demo (will prompt for sudo password)
asciinema rec -t "xcover - Functional Test Coverage Profiler" \
  --command "sudo ./automated-demo.sh" \
  xcover-demo.cast

# Upload to asciinema.org
asciinema upload xcover-demo.cast

# Copy the URL from output (e.g., https://asciinema.org/a/XXXXX)
```

## Add to README

After uploading, add the embed code to README.md (below line 7, after the project description):

```markdown
[![asciicast](https://asciinema.org/a/XXXXX.svg)](https://asciinema.org/a/XXXXX)
```

Replace `XXXXX` with your actual asciinema ID.

## What the Demo Shows

1. Starting xcover profiler in daemon mode
2. Waiting for profiler ready state
3. Running test scenarios (add, multiply, greet functions)
4. Stopping profiler and viewing coverage report
5. Clear coverage percentage output

Duration: ~45 seconds (perfect for README showcase)

## Why This Demo is Impactful

- **Visual proof** of xcover working without code changes
- **Cross-language** support demonstrated (Go binary)
- **No instrumentation** - binary runs normally
- **Clear metrics** - coverage percentage displayed
- **Professional** - clean, narrated, easy to follow
