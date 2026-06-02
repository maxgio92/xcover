//go:build linux && !userspace

// Package bpftime provides support for running xcover with the bpftime
// userspace BPF runtime (https://github.com/eunomia-bpf/bpftime).
//
// This file is compiled when xcover is built WITHOUT the "userspace" build
// tag. It provides stub implementations that return a clear error so that
// callers fail early with an actionable message instead of a missing-symbol
// link error.
//
// To enable userspace BPF support, rebuild with:
//
//	go build -tags userspace .
package bpftime

import "errors"

var errNotBuilt = errors.New("xcover was not built with userspace BPF support; rebuild with -tags userspace")

// EnsureSyscallServer is a no-op stub. It always returns an error indicating
// that this binary was not compiled with userspace BPF support.
func EnsureSyscallServer() error {
	return errNotBuilt
}

// ExtractAgent is a no-op stub. It always returns an error indicating that
// this binary was not compiled with userspace BPF support.
func ExtractAgent() (string, error) {
	return "", errNotBuilt
}
