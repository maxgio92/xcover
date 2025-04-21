---
title: utrace
---	

## utrace

utrace is a userspace function tracer

### Synopsis

utrace is a kernel-assisted low-overhead userspace function tracer.

```
utrace [flags]
```

### Options

```
      --exclude string     Regex pattern to exclude function symbol names
  -h, --help               help for utrace
      --include string     Regex pattern to include function symbol names
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic)
  -p, --path string        Path to the ELF executable
      --pid int            Filter the process by PID (default -1)
      --report             Generate report (as utrace-report.json)
      --status             Periodically print a status of the trace
      --verbose            Enable verbosity (default true)
```

