## xcover merge

Merge coverage reports of the same binary into one

### Synopsis


merge combines coverage reports produced by separate runs (parallel shards,
distributed E2E, retries) into a single aggregate report.

Functions are identified by their offset within the binary named by build_id:
a function counts as covered if it was hit in any input, and cov_by_func is
recomputed over the merged set. Inputs with different build_id values are
refused. A report without a build_id cannot be verified and is refused unless
--allow-missing-build-id is set, in which case the merged report has an empty
build_id.

Symbol aliases share an offset, and each run keeps one name per offset,
normally the one its include pattern left. When two inputs with the same
build_id name one offset differently, the merged report keeps the name that
sorts first in byte order and the other name leaves funcs_traced and
funcs_ack. Without a verified build_id the conflict is refused. The pid field
is kept when every input recorded the same --pid filter and omitted otherwise.

Pass '-' as a path to read one report from standard input. The merged report is
written to standard output unless --output is set.

```
xcover merge [flags] <report.json>...
```

### Options

```
      --allow-missing-build-id   Merge reports even when one has an empty build_id; the result has an empty build_id
  -h, --help                     help for merge
  -o, --output string            Write the merged report to this file instead of stdout
```

### Options inherited from parent commands

```
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover](../README.md)	 - xcover is a functional test coverage profiler

