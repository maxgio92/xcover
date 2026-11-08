# Symbolization

How xcover finds a name and an address for each function. It tries these
sources in order and uses the first that applies.

## 1. ELF symbol table

`.symtab` lists functions with names and virtual addresses. `--include` and
`--exclude` match these names and, for C++ and Rust symbols, their demangled
form; see [symbol-patterns.md](symbol-patterns.md).

## 2. Go `.gopclntab` (stripped Go binaries)

Stripping removes `.symtab` but keeps `.gopclntab`, the Go runtime's
program-counter table, which xcover reads in every format since Go 1.2. The
fallback is automatic, needs no flag, and works with all filters and with
`--scope project`.

## 3. Function recovery (stripped non-Go binaries)

When a binary has neither `.symtab` nor `.gopclntab`, xcover uses
[resurgo](https://github.com/maxgio92/resurgo) to detect function entry
points from `.eh_frame` unwind data. Only high-confidence candidates are
kept, named `func_0x<offset>` because no real name is available.

Recall drops sharply on optimised builds, so prefer a debug file when you
have one.

Recovery refuses `--include`, `--exclude` and `--scope=project`, since
patterns cannot match synthetic names; see
[`symbol filters need a symbol table`](troubleshooting.md#symbol-filters-need-a-symbol-table-this-binary-has-none).
To filter a stripped binary, pass `--debug-path` or trace an unstripped
build.

## 4. Separate debug file

Point `--debug-path` at a debug file matching the stripped binary, such as
`objcopy --only-keep-debug` output or a distribution `-dbg` or debuginfod
artefact:

```shell
xcover run --path ./app --debug-path ./app.debug
```

Names come from the debug file's `.symtab`, or from its DWARF
`DW_TAG_subprogram` entries when it has no symbol table. Probe offsets are
always computed against `--path`.

Both files must carry the same GNU build-id; pass `--no-build-id-check` for
toolchains that omit it, never to silence a mismatch. See
[build-id errors](troubleshooting.md#cannot-verify-the-debug-file-belongs-to-the-executable-and-other-build-id-errors).

Split DWARF (`-gsplit-dwarf`) and `dwz` supplementary files are not
supported.
