# xcover

Profile functional test coverage without instrumenting your binaries.

`xcover` (pronounced 'cross cover') enables to profile functional test coverage, by leveraging kernel instrumentation to probe functions in userland, and it's cross language.
This makes possible to measure coverage on ELF binaries without ecosystem-specific instrumentation like [Go cover](https://go.dev/doc/build-cover) or [LLVM cov](https://llvm.org/docs/CommandGuide/llvm-cov.html) require.

It currently supports languages compiled to ELF.

## Tracing features

### Filter by PID

```shell
xcover --pid PID
```

### Filter by command

```shell
xcover --pid EXE_PATH
```

### Filter in/out functions

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

### Compare with the static analysis

```shell
$ readelf -s -g -W -C xcover | grep pkg\/trace | awk '{ print $8 }'
github.com/maxgio92/xcover/pkg/trace.(*Profiler).attachSampler
github.com/maxgio92/xcover/pkg/trace.(*Profiler).attachSampler.func1
github.com/maxgio92/xcover/pkg/trace.(*Profiler).attachSampler.func1.(*Logger).Fatal.1
github.com/maxgio92/xcover/pkg/trace.(*Profiler).getExePath
github.com/maxgio92/xcover/pkg/trace.cleanComm
github.com/maxgio92/xcover/pkg/trace.NewProfiler
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.func2
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.func2.deferwrap1
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.deferwrap2
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.deferwrap1
github.com/maxgio92/xcover/pkg/trace.(*Profiler).loadSymTable
github.com/maxgio92/xcover/pkg/trace.(*Profiler).getStackTraceByID
github.com/maxgio92/xcover/pkg/trace.(*Profiler).getHumanReadableStackTrace
github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.func1
github.com/maxgio92/xcover/pkg/trace..typeAssert.0
github.com/maxgio92/xcover/pkg/trace..stmp_0
```

we can notice that during this trace period the tracee run 3 of the functions supported by the package `trace`:
1. `github.com/maxgio92/xcover/pkg/trace.(*Profiler).RunProfile.func2`
1. `github.com/maxgio92/xcover/pkg/trace.(*Profiler).getExePath`
1. `github.com/maxgio92/xcover/pkg/trace.(*Profiler).loadSymTable`

### Reporting

It is possible to generate a report of the traced functions and the functions that actually run, by specifying the flag `--report`.

This data can be useful to calculate coverage in integration tests.

The structure of the report is the following:

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
sudo xcover --path myapp --verbose=false --report
`^C5:02PM INF written report to xcover-report.json`
cat xcover-report.json | jq '.cov_by_func'
15.601900739176347
```

## CLI Reference

Please read the [docs](./docs) for the reference to the `xcover` CLI.
