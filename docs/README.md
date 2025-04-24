---
title: xcover
---	

## xcover

xcover is a userspace function tracer

### Synopsis

xcover is a kernel-assisted low-overhead userspace function tracer.

```
xcover [flags]
```

### Options

```
      --exclude string     Regex pattern to exclude function symbol names
  -h, --help               help for xcover
      --include string     Regex pattern to include function symbol names
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
  -p, --path string        Path to the ELF executable
      --pid int            Filter the process by PID (default -1)
      --report             Generate report (as xcover-report.json)
      --status             Periodically print a status of the trace
      --verbose            Enable verbosity (default true)
```

