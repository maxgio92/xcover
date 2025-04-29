# xcover

Profile coverage of functional tests without instrumenting your binaries.

`xcover` (pronounced 'cross cover') enables to profile functional test coverage, by leveraging kernel instrumentation to probe functions in userland, and it's cross language.
This makes possible to measure coverage on ELF binaries without ecosystem-specific instrumentation like [Go cover](https://go.dev/doc/build-cover) or [LLVM cov](https://llvm.org/docs/CommandGuide/llvm-cov.html) require.

![xcover demo](assets/xcover-demo.gif)

## CLI Reference

## xcover

xcover is a functional test coverage profiler

### Synopsis


xcover is a functional test coverage profiler.

Run the 'profile' command to run the profiler that will trace all the functions of the tracee program.
Wait for the profiler to be ready before running your tests, with the 'wait' command.
Once the profiler is ready to trace all the functions, you can start running your tests.
At the end of your tests, the profiler can be stopped and a report being collected.


### Options

```
  -h, --help               help for xcover
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover profile](docs/xcover_profile.md)	 - Profile the functional test coverage of a program
* [xcover wait](docs/xcover_wait.md)	 - Wait for the xcover profiler to be ready



## Filter

### Filter by process

```shell
xcover profile --pid PID
```

### Filter by binary

```shell
xcover profile --path EXE_PATH
```

### Filter functions

For including specific functions:

```shell
xcover profile --path EXE_PATH --include "^github.com/maxgio92/xcover"
```

or excluding some:

```shell
xcover profile --path EXE_PATH --exclude "^runtime.|^internal"
```

For instance, making `xcover` tracing itself (why not?), excluding the Go runtime functions, and logging all the acknowledged functions in realtime:

```shell
$ sudo ./xcover profile --path xcover --verbose --exclude "^runtime.|^internal/|goexit" --include "^github.com/maxgio92/xcover/pkg/trace"
2:59PM INF initializing tracer component=tracer
2:59PM INF collecting functions component=tracee exclude=^runtime.|^internal/|goexit exe_path=xcover include=^github.com/maxgio92/xcover/pkg/trace
2:59PM INF functions collected component=tracee count=27
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).Run
2:59PM INF tracing functions component=tracer
github.com/maxgio92/xcover/pkg/trace.(*UserTracee).Init
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).handleEvent
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).attachUprobes
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).writeReport.WithReportExePath.func5
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).Init
github.com/maxgio92/xcover/pkg/trace.(*UserTracee).ShouldIncludeSymbol
github.com/maxgio92/xcover/pkg/trace.(*UserTracer).printStatusBar
...
```

## Report

It is possible to generate a report by specifying the flag `--report` (enabled by default).

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
$ sudo xcover profile --path myapp --verbose=false --report
`^C5:02PM INF written report to xcover-report.json`
$ cat xcover-report.json | jq '.cov_by_func'
15.601900739176347
```

## Synchronization

It is possible to synchronize on the `xcover` readiness, meaning that userspace can proceed executing the tests because xcover is ready to trace them all.

You can use the `wait` command to wait for the `xcover` profiler to be ready:

```shell
$ xcover profile --path /path/to/bin --report 2>/dev/null &
$ xcover wait
1:30PM INF waiting for the profiler to be ready
1:30PM INF profiler is ready
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

