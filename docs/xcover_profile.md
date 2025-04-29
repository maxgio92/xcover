## xcover profile

Profile the functional test coverage of a program

### Synopsis


profile runs the coverage profiling for functional tests by tracing all the functions supported by the program being tested.
It supports programs compiled to ELF.


```
xcover profile [flags]
```

### Options

```
      --exclude string   Regex pattern to exclude function symbol names
  -h, --help             help for profile
      --include string   Regex pattern to include function symbol names
  -p, --path string      Path to the ELF executable
      --pid int          Filter the process by PID (default -1)
      --report           Generate report (as xcover-report.json) (default true)
      --status           Periodically print a status of the trace (default true)
      --verbose          Enable verbosity
```

### Options inherited from parent commands

```
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover](README.md)	 - xcover is a functional test coverage profiler

