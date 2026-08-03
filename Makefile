PROGRAM := xcover

# dependencies

git = $(shell command -v git || /bin/false)
bpftool = $(shell command -v bpftool || /bin/false)

# general

mkfile_path := $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir := $(patsubst %/,%,$(dir $(mkfile_path)))
OUTPUT := $(current_dir)/pkg/probe/output

ARCH := $(subst x86_64,x86,$(shell uname -m))
GOARCH := $(subst x86,amd64,$(subst aarch64,arm64,$(ARCH)))

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

LDFLAGS = # ASLR and PIE don't hurt. "-linkmode external -extldflags '-no-pie'"
CGO_CFLAGS = "-I $(current_dir)/$(LIBBPFGO)/output" # Include libbpfgo headers.
CGO_LDFLAGS = "-lelf -lz $(current_dir)/$(LIBBPFGO)/output/libbpf/libbpf.a" # Statically link to libbpf.

COMPILE_MODES := dynamic static

.PHONY: $(PROGRAM)
$(PROGRAM): $(LIBBPFGO)-static $(PROGRAM)/bpf $(PROGRAM)/frontend

.PHONY: $(PROGRAM)/frontend
$(PROGRAM)/frontend:
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go build -ldflags=${LDFLAGS} -v -o ${PROGRAM} .

.PHONY: test
test: TEST_PATH ?= ./...
test: $(LIBBPFGO)-static | $(PROGRAM)/bpf
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go test -ldflags=${LDFLAGS} -v $(TEST_PATH)

.PHONY: test-integration
test-integration: TEST_PATH ?= ./...
test-integration: $(LIBBPFGO)-static | $(PROGRAM)/bpf
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go test -tags integration -ldflags=${LDFLAGS} -v $(TEST_PATH)

.PHONY: test-e2e
test-e2e: TEST_PATH ?= ./e2e
test-e2e:
	XCOVER_E2E_BIN=$(current_dir)/$(PROGRAM) \
		go test -count=1 -tags e2e -v $(TEST_PATH)

.PHONY: docs
docs:
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go run docs/docs.go

.PHONY: $(PROGRAM)/bpf
$(PROGRAM)/bpf: $(OUTPUT) $(VMLINUXH)
	clang $(CFLAGS) -g -O2 -c -target bpf \
		-o $(OUTPUT)/trace.bpf.o bpf/trace.bpf.c

.PHONY: $(foreach compile_mode,$(COMPILE_MODES),$(LIBBPFGO)-$(compile_mode))
$(foreach compile_mode,$(COMPILE_MODES),$(LIBBPFGO)-$(compile_mode)):
	if [ -d $(LIBBPFGO) ]; then \
		make -C $(LIBBPFGO) $@; \
        else \
		$(git) submodule init; \
		$(git) submodule update --recursive; \
		make -C $(LIBBPFGO) $@; \
	fi

.PHONY: $(BPFTOOL)
$(BPFTOOL):
	$(git) clone --recurse-submodules $(BPFTOOL_GIT) && \
		make -C $(BPFTOOL)/src && \
		sudo make -C $(BPFTOOL)/src install-bin

.PHONY: $(VMLINUXH)
$(VMLINUXH):
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

$(OUTPUT):
	mkdir -p $(OUTPUT)

# container build

BUILD_IMAGE := ghcr.io/maxgio92/xcover-build@sha256:4b124acd3185c004c066b0f708f0327a60ebe3933d8e2f509528d451a7cfaf49

.PHONY: xcover-container
xcover-container:
	docker run --rm \
		--user $(shell id -u):$(shell id -g) \
		-e GOCACHE=/work/.cache/go-build \
		-v /sys/kernel/btf:/sys/kernel/btf:ro \
		-v $(current_dir):/work \
		-w /work \
		$(BUILD_IMAGE) \
		make xcover

.PHONY: clean
clean:
	rm -rf $(OUTPUT)
	rm -rf $(LIBBPFGO)
	rm bpf/$(VMLINUXH)
