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

