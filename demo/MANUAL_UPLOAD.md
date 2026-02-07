# Manual Upload Steps for Asciinema Demo

## Status

✅ **Completed:**
- xcover binary built
- demo-app binary built
- Simulated cast file created: `demo/xcover-demo.cast`
- README.md updated with placeholder

⚠️ **Requires Manual Completion:**

The asciinema recording requires two manual authentication steps that cannot be automated:

1. **Sudo authentication** - Needed for eBPF operations
2. **Asciinema.org authentication** - Needed for upload

## How to Complete

### Option 1: Upload the Simulated Cast File (Quick)

```bash
# Upload the pre-generated cast file
asciinema upload demo/xcover-demo.cast

# Copy the returned URL (e.g., https://asciinema.org/a/XXXXX)
# Update README.md line 13-15, replacing "PENDING" with the actual ID
```

### Option 2: Create a Real Recording (Recommended)

```bash
cd /home/massimiliano-giovagnoli/.multiclaude/wts/xcover/calm-dolphin

# Ensure binaries are built
make xcover
cd demo && go build -o demo-app demo-app.go && cd ..

# Authenticate with asciinema.org (one-time)
asciinema auth

# Create the actual recording
asciinema rec -t "xcover - Functional Test Coverage Profiler" \
  --command "sudo bash demo/automated-demo.sh" \
  demo/xcover-demo.cast

# Upload it
asciinema upload demo/xcover-demo.cast

# Update README.md with the returned URL
```

## Update README

After getting the asciinema URL, update line 13-15 in README.md:

```markdown
<!-- Demo recording from asciinema.org -->
[![asciicast](https://asciinema.org/a/XXXXX.svg)](https://asciinema.org/a/XXXXX)
```

Replace `XXXXX` with your actual asciinema ID.

## Then Commit and Push

```bash
git add README.md demo/xcover-demo.cast
git commit -m "docs: add asciinema demo recording

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
Co-Authored-By: Massimiliano Giovagnoli <me@maxgio.it>"

git push origin work/bright-otter
```
