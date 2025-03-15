PROGRAM := yap

# dependencies

git = $(shell command -v git || /bin/false)
bpftool = $(shell command -v bpftool || /bin/false)

# general

mkfile_path := $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir := $(patsubst %/,%,$(dir $(mkfile_path)))
OUTPUT := $(current_dir)/output

ARCH := $(subst x86_64,x86,$(shell uname -m))
GOARCH := $(subst x86,amd64,$(ARCH))

# ebpf

VMLINUXH := vmlinux.h
BTFFILE := /sys/kernel/btf/vmlinux

CFLAGS ?= -D__TARGET_ARCH_$(ARCH)

# libbpf

BPFTOOL := bpftool
BPFTOOL_GIT := https://github.com/libbpf/bpftool.git

# libbpfgo

LIBBPFGO_GIT := https://github.com/aquasecurity/libbpfgo.git
LIBBPFGO := libbpfgo

# frontend

CGO_CFLAGS = "-I $(current_dir)/$(LIBBPFGO)/output"
CGO_LDFLAGS = "-lelf -lz $(current_dir)/$(LIBBPFGO)/output/libbpf/libbpf.a"

.PHONY: $(PROGRAM)
$(PROGRAM): $(LIBBPFGO) | $(PROGRAM)/bpf
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go build -v -o ${PROGRAM} .

.PHONY: docs
docs: $(LIBBPFGO)
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go run docs/docs.go

.PHONY: $(PROGRAM)/bpf
$(PROGRAM)/bpf: $(VMLINUXH)
	clang $(CFLAGS) -g -O2 -c -target bpf \
		-o $(OUTPUT)/profile.bpf.o bpf/profile.bpf.c

.PHONY: $(LIBBPFGO)
$(LIBBPFGO):
	{ test -d $(LIBBPFGO) && make -C $(LIBBPFGO) libbpfgo-static; } \
	|| { $(git) submodule init && \
	$(git) submodule update --recursive && \
	make -C $(LIBBPFGO) libbpfgo-static; }

.PHONY: $(BPFTOOL)
$(BPFTOOL):
	$(git) clone --recurse-submodules $(BPFTOOL_GIT) && \
		make -C $(BPFTOOL)/src && \
		sudo make -C $(BPFTOOL)/src install-bin

.PHONY: $(VMLINUXH)
$(VMLINUXH): $(OUTPUT)
ifeq ($(wildcard $(bpftool)),)
	@echo "ERROR: could not find bpftool"
	@exit 1
endif
	@if [ ! -f $(BTFFILE) ]; then \
		echo "ERROR: kernel does not seem to support BTF"; \
		exit 1; \
	fi
	@if [ ! -f bpf/$(VMLINUXH) ]; then \
		echo "INFO: generating $(VMLINUXH) from $(BTFFILE)"; \
		$(bpftool) btf dump file $(BTFFILE) format c > bpf/$(VMLINUXH); \
	fi

.PHONY: $(OUTPUT)
$(OUTPUT):
	mkdir -p $(OUTPUT)

.PHONY: clean
clean:
	rm -rf $(OUTPUT)
	rm -rf $(LIBBPFGO)
	rm bpf/$(VMLINUXH)
