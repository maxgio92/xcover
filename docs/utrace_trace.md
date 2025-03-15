---
title: utrace trace
---	

## utrace trace

trace executes a sampling profiler and returns the userspace functions run by the selected processes

```
utrace trace [flags]
```

### Options

```
  -c, --comm string     Filter the processes by command
  -h, --help            help for trace
  -o, --output string   the format of output (dot, text) (default "dot")
      --pid int         Filter the process by PID
```

### Options inherited from parent commands

```
      --debug   Sets log level to debug
```

### SEE ALSO

* [utrace](README.md)	 - utrace is a userspace function tracer

