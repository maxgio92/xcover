# Number of bench rounds (each round is an independent tracer lifecycle).
COUNT ?= 100

# Project root (one level up from benchmark-userspace/).
ROOTDIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/..)

# Architecture.
ARCH   := $(subst x86_64,x86,$(shell uname -m))
GOARCH := $(subst x86,amd64,$(subst aarch64,arm64,$(ARCH)))

# libbpfgo / libbpf static linking.
LIBBPFGO    := $(ROOTDIR)/libbpfgo
CGO_CFLAGS  = "-I $(LIBBPFGO)/output"
CGO_LDFLAGS = "-lelf -lz $(LIBBPFGO)/output/libbpf/libbpf.a"

# bpftime shared libraries (embedded into the xcover binary at build time).
# Must be built with: make -C $(ROOTDIR) bpftime-libs
BPFTIME_AGENT := $(ROOTDIR)/pkg/bpftime/libs/bpftime-agent.so

.PHONY: bench prereqs libbpfgo-static targets

## bench: build prereqs and run the userspace BPF benchmark suite.
# No sudo required — bpftime runs in userspace.
bench: prereqs
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
	GOARCH=$(GOARCH) \
	BPFTIME_SHM_MEMORY_MB=2048 \
		go test -bench=. -run='^$$' -v \
			-benchtime=1x \
			-count=$(COUNT) \
			.

## prereqs: ensure bpftime libs and C targets are built.
prereqs: libbpfgo-static targets
	@if [ ! -f "$(BPFTIME_AGENT)" ] || [ ! -s "$(BPFTIME_AGENT)" ]; then \
		echo "ERROR: bpftime agent library not found or empty: $(BPFTIME_AGENT)"; \
		echo "       Run: make -C $(ROOTDIR) bpftime-libs"; \
		exit 1; \
	fi

## libbpfgo-static: build libbpfgo and libbpf as a static library.
libbpfgo-static:
	@$(MAKE) -C $(ROOTDIR) libbpfgo-static

## targets: build the C target binaries (shared with kernel benchmark).
targets:
	@$(MAKE) -C $(ROOTDIR)/benchmark all
