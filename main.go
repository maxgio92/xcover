package main

import (
	"embed"
	"fmt"
	"github.com/maxgio92/utrace/pkg/cmd"
	"os"
)

//go:embed output/*
var probeFS embed.FS

const probePathname = "output/trace.bpf.o"

func main() {
	probe, err := probeFS.ReadFile(probePathname)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	cmd.Execute(probe)
}
