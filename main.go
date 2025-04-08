package main

import (
	"embed"

	"github.com/maxgio92/utrace/pkg/cmd"
)

//go:embed output/*
var probeFS embed.FS

const probePath = "output/trace.bpf.o"

func main() {
	cmd.Execute(probePath)
}
