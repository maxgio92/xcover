#!/usr/bin/env bash
set -ue

sha=$(git -C libbpfgo rev-parse origin/devel)
go get github.com/maxgio92/libbpfgo@$sha
