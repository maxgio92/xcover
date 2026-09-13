## xcover

xcover is a functional test coverage profiler

### Synopsis


xcover is a functional test coverage profiler.

Run the 'run' command to run the profiler that will trace all the functions of the tracee program.
Wait for the profiler to be ready before running your tests, with the 'wait' command.
Once the profiler is ready to trace all the functions, you can start running your tests.
At the end of your tests, the profiler can be stopped and a report being collected.


### Options

```
  -h, --help               help for xcover
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover merge](xcover_merge.md)	 - Merge coverage reports of the same binary into one
* [xcover run](xcover_run.md)	 - Run the coverage profiling for a program
* [xcover status](xcover_status.md)	 - Check the xcover profiler status
* [xcover stop](xcover_stop.md)	 - Stop the xcover profiler daemon
* [xcover wait](xcover_wait.md)	 - Wait for the xcover profiler to be ready

