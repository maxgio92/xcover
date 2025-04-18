## utrace

Trace user-defined functions without instrumentation.

### Get symbols statically

#### Requirements

- binutils

```shell
readelf -s -g -W -C FILE
```

### Compare the static report with runtime

Compare the supported symbols with the functions actually run.

#### Filter by PID

```shell
utrace --pid PID
```

#### Filter by command

```shell
utrace --pid COMM
```

#### Filter in/out functions

For including specific functions:

```shell
utrace --comm COMM --include "^github.com/maxgio92/utrace"
```

or excluding some:

```shell
utrace --comm COMM --exclude "^runtime.|^internal"
```

For instance, making `utrace` tracing itself (why not?), excluding the Go runtime functions:

```shell
$ sudo utrace --comm utrace --exclude "^runtime.|^internal/|goexit" --include "^github.com/maxgio92/utrace/pkg/trace"
encoding/binary.(*decoder).value
encoding/binary.Read
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.func2
golang.org/x/sync/errgroup.(*Group).Go.func1
syscall.Syscall6
syscall.fstatat
os.statNolog
os.Stat
github.com/maxgio92/utrace/pkg/trace.(*Profiler).getExePath
github.com/maxgio92/utrace/pkg/trace.(*Profiler).loadSymTable
```

#### Compare with the static analysis

```shell
$ readelf -s -g -W -C utrace | grep pkg\/trace | awk '{ print $8}'
github.com/maxgio92/utrace/pkg/trace.(*Profiler).attachSampler
github.com/maxgio92/utrace/pkg/trace.(*Profiler).attachSampler.func1
github.com/maxgio92/utrace/pkg/trace.(*Profiler).attachSampler.func1.(*Logger).Fatal.1
github.com/maxgio92/utrace/pkg/trace.(*Profiler).getExePath
github.com/maxgio92/utrace/pkg/trace.cleanComm
github.com/maxgio92/utrace/pkg/trace.NewProfiler
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.func2
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.func2.deferwrap1
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.deferwrap2
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.deferwrap1
github.com/maxgio92/utrace/pkg/trace.(*Profiler).loadSymTable
github.com/maxgio92/utrace/pkg/trace.(*Profiler).getStackTraceByID
github.com/maxgio92/utrace/pkg/trace.(*Profiler).getHumanReadableStackTrace
github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.func1
github.com/maxgio92/utrace/pkg/trace..typeAssert.0
github.com/maxgio92/utrace/pkg/trace..stmp_0
```

we can notice that during this trace period the tracee run 3 of the functions supported by the package `trace`:
1. `github.com/maxgio92/utrace/pkg/trace.(*Profiler).RunProfile.func2`
1. `github.com/maxgio92/utrace/pkg/trace.(*Profiler).getExePath`
1. `github.com/maxgio92/utrace/pkg/trace.(*Profiler).loadSymTable`

## CLI Reference

Please read the [docs](./docs) for the reference to the `utrace` CLI.
