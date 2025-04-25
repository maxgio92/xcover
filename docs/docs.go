//go:build docs

package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra/doc"
)

const (
	docsDir      = "docs"
	fileTemplate = `---
title: %s
---	

`
)

var (
	filePrepender = func(filename string) string {
		title := strings.TrimPrefix(
			strings.TrimSuffix(strings.ReplaceAll(filename, "_", " "), ".md"),
			fmt.Sprintf("%s/", docsDir),
		)
		return fmt.Sprintf(fileTemplate, title)
	}
	linkHandler = func(filename string) string {
		if filename == settings.CmdName+".md" {
			return "README.md"
		}
		return filename
	}
)

func main() {
	if err := doc.GenMarkdownTreeCustom(
		cmd.NewRootCmd(
			cmd.NewCommonOptions(
				cmd.WithLogger(log.New(os.Stderr).Level(log.InfoLevel)),
			),
		),
		docsDir,
		filePrepender,
		linkHandler,
	); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err := os.Rename(path.Join(docsDir, settings.CmdName+".md"), path.Join(docsDir, "README.md"))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
