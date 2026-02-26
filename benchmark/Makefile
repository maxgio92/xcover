CC     ?= gcc
CFLAGS ?= -O0 -g

# Number of distinct functions generated for the miss scenario.
N ?= 10000

# Time or iterations spent on each scenario benchmark.
# When time is provided, number of iterations (b.N) is calculated.
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

# Project root (one level up from benchmark/).
ROOTDIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

# Architecture.
ARCH   := $(subst x86_64,x86,$(shell uname -m))
GOARCH := $(subst x86,amd64,$(subst aarch64,arm64,$(ARCH)))

# libbpfgo / libbpf static linking.
LIBBPFGO     := $(ROOTDIR)/libbpfgo
CGO_CFLAGS   = "-I $(LIBBPFGO)/output"
CGO_LDFLAGS  = "-lelf -lz $(LIBBPFGO)/output/libbpf/libbpf.a"

.PHONY: all hit idle miss bench libbpfgo-static clean

all: hit idle miss

## bench: build libbpfgo-static, C targets, and run Go benchmarks.
bench: libbpfgo-static all
	@sudo \
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go test -bench=. -run='^$$' -v -count=1 -benchtime=$(BENCHTIME) .

## libbpfgo-static: build libbpfgo and libbpf as a static library.
libbpfgo-static:
	$(MAKE) -C $(ROOTDIR) libbpfgo-static

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

## clean: remove all generated and compiled files.
clean:
	rm -f target/hit/hit \
	      target/idle/idle \
	      target/miss/miss \
	      target/miss/miss.c
