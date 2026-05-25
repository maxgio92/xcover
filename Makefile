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

BUILD_IMAGE := ghcr.io/maxgio92/xcover-build@sha256:2e8f0e05c419006d3fb3eff2972c4b85004a45b7e9984ee8f89f8ce7799223ea

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

# bpftime userspace BPF runtime
#
# Build the bpftime shared libraries and copy them into the embed directory so
# that "go build" picks them up when compiling xcover with userspace BPF support.
#
# Prerequisites: cmake >= 3.16, a C++17 compiler, libelf, zlib.

BPFTIME_GIT      := https://github.com/eunomia-bpf/bpftime.git
BPFTIME_DIR      := bpftime-src
BPFTIME_BUILD    := $(BPFTIME_DIR)/build
BPFTIME_LIBS_DST := pkg/bpftime/libs
BPFTIME_LIBBPF_C := $(BPFTIME_DIR)/third_party/bpftool/libbpf/src/libbpf.c

.PHONY: bpftime-libs
bpftime-libs:
	@if [ ! -d $(BPFTIME_DIR) ]; then \
		$(git) clone --recurse-submodules $(BPFTIME_GIT) $(BPFTIME_DIR); \
	fi
	# Fix const-qualifier discards in bpftool-bundled libbpf, hard errors under GCC 14+.
	# Upstream fix: libbpf commit f5dcbae (2026-03-12). Remove once bpftime updates submodule.
	python3 -c "\
f = open('$(BPFTIME_LIBBPF_C)', 'r'); s = f.read(); f.close(); \
s = s.replace('\tchar *res;\n',                                               '\tconst char *res;\n',           1); \
s = s.replace('\t\tchar sym_trim[256], *psym_trim = sym_trim, *sym_sfx;\n',   '\t\tchar sym_trim[256], *psym_trim = sym_trim;\n\t\tconst char *sym_sfx;\n', 1); \
s = s.replace('\t\t\tchar *next_path;\n',                                     '\t\t\tconst char *next_path;\n', 1); \
f = open('$(BPFTIME_LIBBPF_C)', 'w'); f.write(s); f.close()"
	# Fix conflicting declaration of bpf_stream_vprintk in bpftool-bundled libbpf vs
	# vmlinux.h generated from kernel 6.15+. The bundled libbpf declares the helper
	# with 5 params; the kernel BTF declares it with 4. Drop the bundled decl; the
	# bpftool skeleton sources do not call bpf_stream_printk/bpf_stream_vprintk.
	python3 -c "\
import re; \
bpf_h = '$(BPFTIME_DIR)/third_party/bpftool/libbpf/src/bpf_helpers.h'; \
f = open(bpf_h, 'r'); s = f.read(); f.close(); \
s = re.sub(r'extern int bpf_stream_vprintk\b[^;]+;\s*', '', s, count=1); \
f = open(bpf_h, 'w'); f.write(s); f.close()"
	# Fix add_bpf_link not propagating bpf_cookie from BPF_LINK_CREATE args.
	# The bpf_link_handler(bpf_link_create_args) constructor never sets attach_cookie,
	# so bpf_get_attach_cookie() always returns 0 for perf-event uprobes.
	# Use the cookie-aware bpf_link_handler(prog_id, target_fd, cookie) constructor
	# directly from add_bpf_link when attach_type == BPF_PERF_EVENT (41).
	python3 -c "\
f = open('$(BPFTIME_DIR)/runtime/src/bpftime_shm_internal.cpp', 'r'); s = f.read(); f.close(); \
s = s.replace(\
	'\treturn manager->set_handler(fd, bpftime::bpf_link_handler(*args),\n\t\t\t\t    segment);\n}', \
	'\tif (args->attach_type == 41) {\n\t\treturn manager->set_handler(fd,\n\t\t\tbpftime::bpf_link_handler(args->prog_fd, args->target_fd,\n\t\t\t\tstd::optional<uint64_t>(args->perf_event.bpf_cookie)),\n\t\t\tsegment);\n\t}\n\treturn manager->set_handler(fd, bpftime::bpf_link_handler(*args),\n\t\t\t\t    segment);\n}', \
	1); \
f = open('$(BPFTIME_DIR)/runtime/src/bpftime_shm_internal.cpp', 'w'); f.write(s); f.close()"
	cmake -B $(BPFTIME_BUILD) -S $(BPFTIME_DIR) \
		-DCMAKE_BUILD_TYPE=Release \
		-DBPFTIME_UBPF_JIT=ON \
		-DBPFTIME_LLVM_JIT=OFF
	# Prepend llvm-config --libdir so the built bpftool can resolve libLLVM.so
	# when invoked at skel.h generation time. No-op when llvm-config is absent.
	LD_LIBRARY_PATH="$$(llvm-config --libdir 2>/dev/null):$$LD_LIBRARY_PATH" \
		cmake --build $(BPFTIME_BUILD) --parallel
	cp $(BPFTIME_BUILD)/runtime/syscall-server/libbpftime-syscall-server.so \
		$(BPFTIME_LIBS_DST)/bpftime-syscall-server.so
	cp $(BPFTIME_BUILD)/runtime/agent/libbpftime-agent.so \
		$(BPFTIME_LIBS_DST)/bpftime-agent.so
	@echo "bpftime libraries copied to $(BPFTIME_LIBS_DST)"

.PHONY: clean
clean:
	rm -rf $(OUTPUT)
	rm -rf $(LIBBPFGO)
	rm bpf/$(VMLINUXH)

.PHONY: clean-bpftime
clean-bpftime:
	rm -rf $(BPFTIME_DIR)
