## xcover merge

Merge coverage reports of the same binary into one

### Synopsis


merge combines coverage reports produced by separate runs (parallel shards,
distributed E2E, retries) into a single aggregate report.

Functions are identified by their offset within the binary named by build_id:
a function counts as covered if it was hit in any input, and cov_by_func is
recomputed over the merged set. Every input must carry the same build_id;
reports with a different or empty build_id are refused unless
--allow-mismatched-build-id is set, in which case the merged report has an
empty build_id.

Pass '-' as a path to read one report from standard input. The merged report is
written to standard output unless --output is set.

```
xcover merge [flags] <report.json>...
```

### Options

```
      --allow-mismatched-build-id   Merge reports whose build_id differs or is empty; the result has an empty build_id
  -h, --help                        help for merge
  -o, --output string               Write the merged report to this file instead of stdout
```

### Options inherited from parent commands

```
      --log-level string   Log level (trace, debug, info, warn, error, fatal, panic) (default "info")
```

### SEE ALSO

* [xcover](../README.md)	 - xcover is a functional test coverage profiler

