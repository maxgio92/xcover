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

BPFTIME := bpftime-libs

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

# xcover-userspace: build with bpftime userspace BPF support.
.PHONY: $(PROGRAM)-userspace
$(PROGRAM)-userspace: $(LIBBPFGO)-static $(BPFTIME) $(PROGRAM)/bpf
	CC=gcc \
	CGO_CFLAGS=$(CGO_CFLAGS) \
	CGO_LDFLAGS=$(CGO_LDFLAGS) \
		GOARCH=$(GOARCH) \
		go build -tags userspace -ldflags=${LDFLAGS} -v -o ${PROGRAM}-userspace .

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
	[ -e $(LIBBPFGO)/.git ] || $(git) submodule update --init --recursive
	make -C $(LIBBPFGO) $@

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

BUILD_IMAGE := ghcr.io/maxgio92/xcover-build@sha256:049a5a98c1f739c86bd7c5ec3a133e53c0d707ba8e175c0638782d1cc5cee7db
BUILD_IMAGE_USERSPACE := ghcr.io/maxgio92/xcover-build@sha256:c97795774ad0aa3d9208734bf0b1740a268202c190bffc67fb53a5955b2a269b

define build-in-container
	$(git) submodule update --init --recursive
	docker run --rm \
		--user $(shell id -u):$(shell id -g) \
		-e GOCACHE=/work/.cache/go-build \
		-v /sys/kernel/btf:/sys/kernel/btf:ro \
		-v $(current_dir):/work:z \
		-w /work \
		$(1) \
		make $(2)
endef

.PHONY: xcover-container
xcover-container:
	$(call build-in-container,$(BUILD_IMAGE),xcover)

.PHONY: xcover-container-userspace
xcover-container-userspace:
	$(call build-in-container,$(BUILD_IMAGE_USERSPACE),xcover-userspace)

# bpftime userspace BPF runtime
#
# Build the bpftime shared libraries and copy them into the embed directory so
# that "go build" picks them up when compiling xcover with userspace BPF support.
#
# Prerequisites: cmake >= 3.16, a C++17 compiler, libelf, zlib.

BPFTIME_GIT      := https://github.com/eunomia-bpf/bpftime.git
# Pinned to 5bf24b21af85 (2026-05-25): includes fix for bpf_link attach_cookie and
# FEAT_PERF_LINK detection (PR #570). Bump this when pulling in further upstream fixes.
BPFTIME_COMMIT   := 5bf24b21af85efee0443710c4dd4a34f3b92e0a2
BPFTIME_DIR      := bpftime-src
BPFTIME_BUILD    := $(BPFTIME_DIR)/build
BPFTIME_LIBS_DST := pkg/bpftime/libs
# Prefer brew-installed llvm@18 (compatible with bpftime's LLVM JIT) over any
# system LLVM. If llvm@18 is not installed, LLVM18_PREFIX is empty and the
# cmake flags below are omitted, falling back to whatever cmake finds.
LLVM18_PREFIX    := $(shell brew --prefix llvm@18 2>/dev/null)

.PHONY: bpftime-libs
bpftime-libs:
	@if [ ! -d $(BPFTIME_DIR) ]; then \
		$(git) clone --recurse-submodules $(BPFTIME_GIT) $(BPFTIME_DIR); \
		$(git) -C $(BPFTIME_DIR) checkout $(BPFTIME_COMMIT); \
		$(git) -C $(BPFTIME_DIR) submodule update --init --recursive; \
		for p in patches/bpftime/*.patch; do \
			echo "Applying $$p"; \
			patch -p1 -d $(BPFTIME_DIR) < $$p || exit 1; \
		done; \
	fi
	cmake -B $(BPFTIME_BUILD) -S $(BPFTIME_DIR) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBPFTIME_UBPF_JIT=ON \
		-DBPFTIME_LLVM_JIT=ON \
		-DCMAKE_EXE_LINKER_FLAGS=-no-pie \
		$(if $(LLVM18_PREFIX),-DLLVM_DIR=$(LLVM18_PREFIX)/lib/cmake/llvm) \
		$(if $(LLVM18_PREFIX),-DCMAKE_C_COMPILER=$(LLVM18_PREFIX)/bin/clang) \
		$(if $(LLVM18_PREFIX),-DCMAKE_CXX_COMPILER=$(LLVM18_PREFIX)/bin/clang++)
	# Prepend llvm@18 (and fallback llvm-config) libdir so bpftool can resolve
	# libLLVM.so at skel.h generation time.
	LD_LIBRARY_PATH="$(LLVM18_PREFIX)/lib:$$(llvm-config --libdir 2>/dev/null):$$LD_LIBRARY_PATH" \
		cmake --build $(BPFTIME_BUILD) --parallel
	mkdir -p $(BPFTIME_LIBS_DST)
	cp $(BPFTIME_BUILD)/runtime/syscall-server/libbpftime-syscall-server.so \
		$(BPFTIME_LIBS_DST)/bpftime-syscall-server.so
	cp $(BPFTIME_BUILD)/runtime/agent/libbpftime-agent.so \
		$(BPFTIME_LIBS_DST)/bpftime-agent.so
	@echo "bpftime libraries copied to $(BPFTIME_LIBS_DST)"

.PHONY: clean
clean: clean-bpftime
	rm -rf $(OUTPUT)
	rm -rf $(LIBBPFGO)
	rm -f bpf/$(VMLINUXH)

.PHONY: clean-bpftime
clean-bpftime:
	rm -rf $(BPFTIME_DIR)
	rm -f $(current_dir)/pkg/bpftime/libs/bpftime-syscall-server.so
	rm -f $(current_dir)/pkg/bpftime/libs/bpftime-agent.so
