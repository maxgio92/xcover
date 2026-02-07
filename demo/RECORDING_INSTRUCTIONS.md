# xcover Asciinema Demo Recording Instructions

## Prerequisites

- asciinema installed (`brew install asciinema`)
- xcover built in the repository
- jq installed for JSON parsing
- sudo access for eBPF operations

## Quick Recording

Run the provided script:

```bash
cd /tmp/claude-1000/-home-massimiliano-giovagnoli--multiclaude-wts-xcover-bright-otter/dc713876-a77e-4dd7-bf46-04b3fd0af41f/scratchpad
./record-demo.sh
```

This will:
1. Prepare the demo environment
2. Start asciinema recording
3. Guide you through the demo steps
4. Save the recording to `/tmp/xcover-demo.cast`

## Manual Recording (Alternative)

If you prefer to record manually:

```bash
# 1. Start recording
asciinema rec -t "xcover - Functional Test Coverage Profiler" /tmp/xcover-demo.cast

# 2. Follow these steps in the terminal:
clear

echo "=== xcover: Functional Test Coverage Profiler ==="
echo "# A cross-language coverage profiler using eBPF"
echo

# Start profiler
sudo /home/massimiliano-giovagnoli/.multiclaude/wts/xcover/bright-otter/xcover run --detach --path /tmp/claude-1000/-home-massimiliano-giovagnoli--multiclaude-wts-xcover-bright-otter/dc713876-a77e-4dd7-bf46-04b3fd0af41f/scratchpad/demo-app

# Wait for ready
sudo /home/massimiliano-giovagnoli/.multiclaude/wts/xcover/bright-otter/xcover wait

# Run test scenarios
/tmp/claude-1000/-home-massimiliano-giovagnoli--multiclaude-wts-xcover-bright-otter/dc713876-a77e-4dd7-bf46-04b3fd0af41f/scratchpad/demo-app add
/tmp/claude-1000/-home-massimiliano-giovagnoli--multiclaude-wts-xcover-bright-otter/dc713876-a77e-4dd7-bf46-04b3fd0af41f/scratchpad/demo-app multiply
/tmp/claude-1000/-home-massimiliano-giovagnoli--multiclaude-wts-xcover-bright-otter/dc713876-a77e-4dd7-bf46-04b3fd0af41f/scratchpad/demo-app greet

# Stop and view report
sudo /home/massimiliano-giovagnoli/.multiclaude/wts/xcover/bright-otter/xcover stop
cat xcover-report.json | jq ".cov_by_func"

# 3. Exit recording with Ctrl+D
```

## Upload to asciinema.org

```bash
asciinema upload /tmp/xcover-demo.cast
```

This will output a URL like: `https://asciinema.org/a/XXXXX`

## Add to README

Use the embed code in the README:

```markdown
[![asciicast](https://asciinema.org/a/XXXXX.svg)](https://asciinema.org/a/XXXXX)
```

## Tips for Best Results

- Keep the terminal window at a reasonable size (100x30 is good)
- Speak slowly and clearly when typing commands
- Use `sleep` commands between steps for better readability
- Keep the demo under 2 minutes total
- Show clear, impactful results (coverage percentage)
