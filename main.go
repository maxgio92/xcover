package main

import (
	"embed"

	"github.com/maxgio92/xcover/pkg/cmd"
)

//go:embed output/*
var probeFS embed.FS

func main() {
	data, err := probeFS.ReadFile("output/trace.bpf.o")
	if err != nil {
		panic(err)
	}
	cmd.Execute(data, "trace.bpf.o")
}
