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
  -p, --comm string      Path to the ELF executable
      --debug            Sets log level to debug
      --exclude string   Regex pattern to exclude function symbol names
  -h, --help             help for utrace
      --include string   Regex pattern to include function symbol names
      --pid int          Filter the process by PID (default -1)
```

