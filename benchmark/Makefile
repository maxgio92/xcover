CC     ?= gcc
CFLAGS ?= -O0 -g

# Number of distinct functions generated for the miss scenario.
N ?= 10000

# Time or iterations spent on each scenario benchmark.
# When time is provided, number of iterations (b.N) is calculated.
# Iterations share the BPF state, that is, uprobes and maps are
# loaded and unloaded just once across all.
#
# To accurately measure the miss scenario, b.N must be 1.
#
# Since the uprobes and BPF maps are unloaded only at teardown
# of each benchmark, the miss scenario must be called just once,
# otherwise the mean time gets polluted with function hit cases,
# coming from the subsequent benchmark iterations.
#
# pollution = (b.N - 1) / b.N
#
# Miss benchtime must be exactly equal to either:
# * miss function execution time * unique function count,
# * or, 1x, that is: run the benchmark once.
#
# To be precise, given:
# * N         - number of internal function calls per C binary run
# * t_hit     - ns/call measured on the hit path
# * t_miss    - ns/call measured on the miss path
# * T_hit     - wall time for one b.N iteration after the first: N × t_hit
# * T_miss    - wall time for the first b.N iteration (all genuine misses): N × t_miss
# * B         - benchtime, total wall time budget for the benchmark (e.g. 10s = 10,000,000,000 ns)
# * b.N       - number of times Go's benchmark framework runs the iteration loop
# * pollution - fraction of b.N iterations that measure the hit path instead of the miss path
# * avg       - the ns/call value the benchmark actually reports
#
# And:
# N = 10,000
# t_miss = 2,674 ns
# t_hit = 1,151 ns ns
#
# And the consequent calculations:
# T_miss = N × t_miss = 10,000 × 2,674 ns ≈ 26.7 ms
# T_hit  = N × t_hit  = 10,000 × 1,151 ns ≈ 11.5 ms
#
# b.N        = 1 + floor((B - T_miss) / T_hit)
# pollution  = (b.N - 1) / b.N
# avg        = (t_miss + (b.N - 1) × t_hit) / b.N
#
# Then:
# * as B → ∞, avg → t_hit
#
# So:
# * b.N must be 1 to accurately bench the miss scenario.
BENCHTIME ?= 1x

# Count do not share the BPF state, that is, uprobes and maps are
# loaded and unloaded at every bench round (e.g. count=100, 100 load/unload).
# -benchtime=1x  run each benchmark for exactly 1 iteration (b.N=1). Prevents
#                the seen_funcs BPF map from carrying over into subsequent
#                iterations, which would turn miss measurements into hit ones.
# -count=100     call each benchmark 100 times independently, restarting the
#                tracer and resetting the BPF map each time. Produces 100 clean
#                samples per scenario for reliable p50/p99 estimation.
COUNT ?= 100

# Project root (one level up from benchmark/).
ROOTDIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

# Architecture.
ARCH   := $(subst x86_64,x86,$(shell uname -m))
GOARCH := $(subst x86,amd64,$(subst aarch64,arm64,$(ARCH)))

# libbpfgo / libbpf static linking.
LIBBPFGO     := $(ROOTDIR)/libbpfgo
CGO_CFLAGS   = "-I $(LIBBPFGO)/output"
CGO_LDFLAGS  = "-lelf -lz $(LIBBPFGO)/output/libbpf/libbpf.a"

# bpftime shared libraries (embedded into the xcover binary at build time).
# Must be built with: make -C $(ROOTDIR) bpftime-libs
BPFTIME_AGENT := $(ROOTDIR)/pkg/bpftime/libs/bpftime-agent.so

.PHONY: all hit idle miss bench bench-kernel bench-userspace libbpfgo-static clean clean-miss bench-report bench-compare

all: hit idle miss

## bench-kernel: build libbpfgo-static, C targets, and run kernel uprobe benchmarks.
## Requires sudo (CAP_BPF + CAP_PERFMON).
bench bench-kernel: libbpfgo-static
	$(MAKE) clean-miss
	$(MAKE) all N=$(N)
	@sudo \
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go test -bench=. -run='^$$' -v \
			-benchtime=$(BENCHTIME) \
			-count=$(COUNT) \
			.
	$(MAKE) clean

## bench-userspace: build prereqs and run userspace BPF benchmarks (no sudo).
## Requires bpftime libs: make -C $(ROOTDIR) bpftime-libs
bench-userspace: libbpfgo-static
	@if [ ! -f "$(BPFTIME_AGENT)" ] || [ ! -s "$(BPFTIME_AGENT)" ]; then \
		echo "ERROR: bpftime agent library not found: $(BPFTIME_AGENT)"; \
		echo "       Run: make -C $(ROOTDIR) bpftime-libs"; \
		exit 1; \
	fi
	$(MAKE) clean-miss
	$(MAKE) all N=$(N)
	rm -f /dev/shm/bpftime_maps_shm
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
	GOARCH=$(GOARCH) \
	BPFTIME_SHM_MEMORY_MB=2048 \
	BPFTIME_MAX_FD_COUNT=25000 \
		go test -tags userspace -bench=. -run='^$$' -v \
			-benchtime=1x \
			-count=$(COUNT) \
			.
	$(MAKE) clean

## libbpfgo-static: build libbpfgo and libbpf as a static library.
libbpfgo-static:
	@$(MAKE) -C $(ROOTDIR) libbpfgo-static

## hit: compile the hit scenario target binary.
hit: target/hit/hit

target/hit/hit: target/hit/main.c
	$(CC) $(CFLAGS) -o $@ $<

## idle: compile the idle scenario target binary.
idle: target/idle/idle

target/idle/idle: target/idle/main.c
	$(CC) $(CFLAGS) -o $@ $<

## miss: generate and compile the miss scenario target binary.
miss: target/miss/miss

target/miss/miss.c: target/miss/gen/main.go
	go run $< -n $(N) > $@

target/miss/miss: target/miss/miss.c
	$(CC) $(CFLAGS) -o $@ $<

## clean-miss: remove generated miss source and binary so N is re-applied on next build.
clean-miss:
	rm -f target/miss/miss target/miss/miss.c

## bench-report: run kernel and userspace benchmarks, write JSON reports
## Requires bpftime libs (make -C .. bpftime-libs), sudo for the kernel path.
bench-report:
	@mkdir -p results
	$(MAKE) bench-kernel N=100 COUNT=10    \
		| grep --line-buffered -E '^(goos:|goarch:|pkg:|cpu:|Benchmark|PASS|FAIL|ok)' \
		| tee results/kernel.txt
	$(MAKE) bench-userspace N=100 COUNT=10 \
		| grep --line-buffered -E '^(goos:|goarch:|pkg:|cpu:|Benchmark|PASS|FAIL|ok)' \
		| tee results/userspace.txt
## bench-compare: compare kernel and userpsace benchmarks with benchstat.
## Requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest.
bench-compare:
	@benchstat -filter '.unit:ns/call' \
		results/kernel.txt \
		results/userspace.txt

## clean: remove all generated and compiled files.
clean:
	rm -f target/hit/hit \
	      target/idle/idle \
	      target/miss/miss \
	      target/miss/miss.c
