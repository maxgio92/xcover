CC     ?= gcc
CFLAGS ?= -O0 -g

# Number of distinct functions generated for the miss scenario.
N ?= 10000

# Time spent on each scenario.
BENCHTIME ?= 10s

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
	sudo \
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
