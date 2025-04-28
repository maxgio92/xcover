# xcover

Profile coverage of functional tests without instrumenting your binaries.

`xcover` (pronounced 'cross cover') enables to profile functional test coverage, by leveraging kernel instrumentation to probe functions in userland, and it's cross language.
This makes possible to measure coverage on ELF binaries without ecosystem-specific instrumentation like [Go cover](https://go.dev/doc/build-cover) or [LLVM cov](https://llvm.org/docs/CommandGuide/llvm-cov.html) require.

## Filter

### Filter by process

```shell
xcover --pid PID
```

### Filter by binary

```shell
xcover --path EXE_PATH
```

### Filter functions

For including specific functions:

```shell
xcover --path EXE_PATH --include "^github.com/maxgio92/xcover"
```

or excluding some:

```shell
xcover --path EXE_PATH --exclude "^runtime.|^internal"
```

For instance, making `xcover` tracing itself (why not?), excluding the Go runtime functions:

```shell
$ sudo xcover --path xcover --exclude "^runtime.|^internal/|goexit" --include "^github.com/maxgio92/xcover/pkg/trace"
encoding/binary.(*decoder).value
encoding/binary.Read
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.func2
golang.org/x/sync/errgroup.(*Group).Go.func1
syscall.Syscall6
syscall.fstatat
os.statNolog
os.Stat
github.com/maxgio92/xcover/pkg/trace.(*Profiler).getExePath
github.com/maxgio92/xcover/pkg/trace.(*Profiler).loadSymTable
```

## Report

It is possible to generate a report by specifying the flag `--report`.

The report is provided in JSON format and contains
* the functions that have been traced
* the functions acknowledged
* the coverage by function percentage
* the executable path

```go
type CoverageReport struct {
	FuncsTraced []string `json:"funcs_traced"`
	FuncsAck    []string `json:"funcs_ack"`
	CovByFunc   float64  `json:"cov_by_func"`
	ExePath     string   `json:"exe_path"`
}
```

For instance:

```shell
$ sudo xcover --path myapp --verbose=false --report
`^C5:02PM INF written report to xcover-report.json`
$ cat xcover-report.json | jq '.cov_by_func'
15.601900739176347
```

### Synchronization

It is possible to synchronize on the `xcover` readiness, meaning that userspace can proceed executing the tests because xcover is ready to trace them all.

You can use the `wait` command to wait for the `xcover` profiler to be ready:

```shell
$ xcover --path /path/to/bin --report &
9:01PM INF waiting for xcover to be ready
9:01PM INF xcover is ready
$ /path/to/bin test_1
$ /path/to/bin test_2
$ /path/to/bin test_3
```

and collect the coverage:

```shell
coverage=$(jq '.cov_by_func' < <(cat xcover-report.json))
if [[ $(echo "$coverage < 70" | bc -l) != $true ]]; then
  echo "coverage too low"
fi
```

### Progressive status

It is possible to show a progressive status during the profiling `xcover` runs via the flag `--status`.

```
$ sudo xcover --status --verbose=false --report --path ./myapp
Functions aknowledged: [███████                                 ]  18.31% Events/s:   37       Events Buffer: [          ]   0% Feed Buffer: [          ]   0%
```

## CLI Reference

Please read the [docs](./docs) for the reference to the `xcover` CLI.
