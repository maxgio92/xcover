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
	[ -d $(LIBBPFGO)/.git ] || $(git) submodule update --init --recursive
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

BUILD_IMAGE          := ghcr.io/maxgio92/xcover-build@sha256:fd798eb6ab7304cb85bc06d5418bc479abf9294c370682b5518456d81a7451f3
BUILD_IMAGE_USERSPACE := ghcr.io/maxgio92/xcover-build@sha256:70fc8d1a7418356eb6cadadb44027ceb5d3fb718828d454e126b4b80bfd229b6

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
BPFTIME_COMMIT   := 5bf24b21af856f79a6aa3bd8da6e4dcfbe1d95d4
BPFTIME_DIR      := bpftime-src
BPFTIME_BUILD    := $(BPFTIME_DIR)/build
BPFTIME_LIBS_DST := pkg/bpftime/libs
BPFTIME_LIBBPF_C := $(BPFTIME_DIR)/third_party/bpftool/libbpf/src/libbpf.c
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
	fi
	# Fix const-qualifier discards in bpftool-bundled libbpf, hard errors under GCC 14+.
	# Upstream fix: libbpf commit f5dcbae (2026-03-12). Remove once bpftime bumps its
	# bpftool submodule past that date.
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
	# Remove once bpftime bumps its bpftool submodule past bpftool commit 640fb7ceed18
	# (2025-11-10).
	python3 -c "\
import re; \
bpf_h = '$(BPFTIME_DIR)/third_party/bpftool/libbpf/src/bpf_helpers.h'; \
f = open(bpf_h, 'r'); s = f.read(); f.close(); \
s = re.sub(r'extern int bpf_stream_vprintk\b[^;]+;\s*', '', s, count=1); \
f = open(bpf_h, 'w'); f.write(s); f.close()"
	# Fix __destruct_shm crash when injected_pids is null (LD_PRELOAD agent path).
	# open_type is set in the member initializer list before the constructor body,
	# so even a partially-constructed bpftime_shm has open_type == SHM_OPEN_ONLY.
	# If the SHM open fails (or is never initialised), injected_pids stays null and
	# erase() crashes the tracee on exit.  Guard both sides defensively.
	python3 -c "\
shm = '$(BPFTIME_DIR)/runtime/src/bpftime_shm_internal.cpp'; \
f = open(shm, 'r'); s = f.read(); f.close(); \
s = s.replace( \
	'\tif (bpftime::shm_holder.global_shared_memory.get_open_type() ==\n\t    bpftime::shm_open_type::SHM_OPEN_ONLY) {', \
	'\tif (global_shm_initialized &&\n\t    bpftime::shm_holder.global_shared_memory.get_open_type() ==\n\t    bpftime::shm_open_type::SHM_OPEN_ONLY) {', \
	1); \
s = s.replace( \
	'void bpftime_shm::remove_pid_from_alive_agent_set(int pid)\n{\n\tinjected_pids->erase(pid);\n}', \
	'void bpftime_shm::remove_pid_from_alive_agent_set(int pid)\n{\n\tif (injected_pids != nullptr) {\n\t\tinjected_pids->erase(pid);\n\t}\n}', \
	1); \
f = open(shm, 'w'); f.write(s); f.close()"
	# Fix OOB crash in handler_manager when the OS fd value exceeds max_fd_count.
	# set_handler() calls is_allocated() which returns false for fd >= size, then
	# does handlers[fd] — an unchecked OOB write.  get_handler() / operator[] also
	# index directly without a bounds check.  Add guards to all three so that
	# exhausting the handler slots returns -ENOSPC (or an unused_handler sentinel)
	# instead of crashing the tracer process with a SIGSEGV.
	# Also patch open_fake_fd() to close the real /dev/null fd and return -1 when
	# it would exceed the array bounds, avoiding an OS fd leak on top of the crash.
	python3 -c "\
hm = '$(BPFTIME_DIR)/runtime/src/handler/handler_manager.cpp'; \
f = open(hm, 'r'); s = f.read(); f.close(); \
s = s.replace( \
	'int handler_manager::set_handler(int fd, handler_variant &&handler,\n\t\t\t\t managed_shared_memory &memory)\n{\n\tif (is_allocated(fd)) {', \
	'int handler_manager::set_handler(int fd, handler_variant &&handler,\n\t\t\t\t managed_shared_memory &memory)\n{\n\tif (fd < 0 || (std::size_t)fd >= handlers.size()) {\n\t\tSPDLOG_ERROR(\"set_handler: fd {} out of range [0, {})\", fd, handlers.size());\n\t\treturn -ENOSPC;\n\t}\n\tif (is_allocated(fd)) {', \
	1); \
s = s.replace( \
	'const handler_variant &handler_manager::get_handler(int fd) const\n{\n\treturn handlers[fd];\n}', \
	'const handler_variant &handler_manager::get_handler(int fd) const\n{\n\tstatic const handler_variant oob_handler = unused_handler{};\n\tif (fd < 0 || (std::size_t)fd >= handlers.size()) return oob_handler;\n\treturn handlers[fd];\n}', \
	1); \
s = s.replace( \
	'const handler_variant &handler_manager::operator[](int idx) const\n{\n\treturn handlers[idx];\n}', \
	'const handler_variant &handler_manager::operator[](int idx) const\n{\n\tstatic const handler_variant oob_handler = unused_handler{};\n\tif (idx < 0 || (std::size_t)idx >= handlers.size()) return oob_handler;\n\treturn handlers[idx];\n}', \
	1); \
f = open(hm, 'w'); f.write(s); f.close()"
	python3 -c "\
shm = '$(BPFTIME_DIR)/runtime/src/bpftime_shm_internal.cpp'; \
f = open(shm, 'r'); s = f.read(); f.close(); \
s = s.replace( \
	'int bpftime_shm::open_fake_fd()\n{\n\tint fd = open(\"/dev/null\", O_RDONLY);\n\tint cnt = 5;\n\twhile (fd <= 2 && fd >= 0 && --cnt > 0) {\n\t\tfd = dup(fd);\n\t}\n\treturn fd;\n}', \
	'int bpftime_shm::open_fake_fd()\n{\n\tint fd = open(\"/dev/null\", O_RDONLY);\n\tint cnt = 5;\n\twhile (fd <= 2 && fd >= 0 && --cnt > 0) {\n\t\tfd = dup(fd);\n\t}\n\tif (fd >= 0 && (std::size_t)fd >= manager->size()) {\n\t\tclose(fd);\n\t\terrno = ENOSPC;\n\t\treturn -1;\n\t}\n\treturn fd;\n}', \
	1); \
f = open(shm, 'w'); f.write(s); f.close()"
	# Fix perf event handler slot leak on BPF link close.
	# When a BPF_PERF_EVENT link is destroyed (close(link_fd)), libbpf relies on
	# the kernel to drop the perf event reference. In bpftime userspace that never
	# happens: clear_id_at() for a bpf_link_handler only freed the link slot,
	# leaving the perf event handler permanently allocated — one slot leaked per
	# uprobe per attach/detach cycle, exhausting the pool across benchmark rounds.
	# Fix: cascade the clear to the attached perf event handler. The link slot is
	# set to unused_handler first to prevent infinite recursion (the perf event
	# cleanup scans for linked handlers by attach_target_id).
	python3 -c "\
hm = '$(BPFTIME_DIR)/runtime/src/handler/handler_manager.cpp'; \
f = open(hm, 'r'); s = f.read(); f.close(); \
s = s.replace( \
	'\t\t\t\tclear_id_at(i, memory);\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t}\n\thandlers[fd] = unused_handler();\n}', \
	'\t\t\t\tclear_id_at(i, memory);\n\t\t\t\t}\n\t\t\t}\n\t\t}\n\t} else if (std::holds_alternative<bpf_link_handler>(handlers[fd])) {\n\t\tauto target_fd =\n\t\t\tstd::get<bpf_link_handler>(handlers[fd]).attach_target_id;\n\t\thandlers[fd] = unused_handler();\n\t\tSPDLOG_DEBUG(\"Destroying link handler {}, cascading to perf event {}\", fd, target_fd);\n\t\tclear_id_at(target_fd, memory);\n\t\treturn;\n\t}\n\thandlers[fd] = unused_handler();\n}', \
	1); \
f = open(hm, 'w'); f.write(s); f.close()"
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
