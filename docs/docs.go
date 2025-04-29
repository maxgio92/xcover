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
	docsDir            = "docs"
	fileTemplateHeader = `` // Use it for headers like YAML frontmatters.
	templateMarker     = "{{ .CLI_REFERENCE }}"
)

var (
	filePrepender = func(filename string) string {
		if fileTemplateHeader == "" {
			return ""
		}
		title := strings.TrimPrefix(
			strings.TrimSuffix(strings.ReplaceAll(filename, "_", " "), ".md"),
			fmt.Sprintf("%s/", docsDir),
		)
		return fmt.Sprintf(fileTemplateHeader, title)
	}
	linkHandler = func(filename string) string {
		if filename == settings.CmdName+".md" {
			// This is the root command.
			return "README.md"
		}
		// Otherwise prefix with docs/.
		return path.Join("docs", filename)
	}
)

func main() {
	cmdDocsPath := path.Join(docsDir, settings.CmdName+".md")

	// Generate CLI docs
	if err := doc.GenMarkdownTreeCustom(
		cmd.NewCommand(
			cmd.NewOptions(
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

	// Read the original (handwritten) README
	readmeBytes, err := os.ReadFile("README.md.tpl")
	if err != nil {
		fmt.Println("failed to read README template:", err)
		os.Exit(1)
	}
	readme := string(readmeBytes)

	// Read the CLI generated docs
	cmdDocsBytes, err := os.ReadFile(cmdDocsPath)
	if err != nil {
		fmt.Println("failed to read CLI doc README:", err)
		os.Exit(1)
	}
	cmdDocs := string(cmdDocsBytes)

	// Replace the template marker
	finalReadme := strings.Replace(readme, templateMarker, cmdDocs, 1)

	// Write the final README
	err = os.WriteFile("README.md", []byte(finalReadme), 0644)
	if err != nil {
		fmt.Println("failed to write final README:", err)
		os.Exit(1)
	}
}
