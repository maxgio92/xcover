//go:build docs

package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/maxgio92/xcover/internal/settings"
	"github.com/maxgio92/xcover/pkg/cmd"
	"github.com/maxgio92/xcover/pkg/cmd/options"

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
	// Links are written relative to docs/, where the generated pages live.
	// The root command page is embedded into README.md at the repository
	// root, so its links are rewritten with a docs/ prefix in main.
	linkHandler = func(filename string) string {
		if filename == settings.CmdName+".md" {
			// This is the root command.
			return "../README.md"
		}
		return filename
	}
)

func main() {
	cmdDocsPath := path.Join(docsDir, settings.CmdName+".md")

	// Generate CLI docs
	if err := doc.GenMarkdownTreeCustom(
		cmd.NewCommand(options.NewOptions(
			options.WithLogger(log.New(os.Stderr).Level(log.InfoLevel)),
		)),
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
	// Rewrite subcommand links so they resolve from the repository root.
	cmdDocs := strings.ReplaceAll(string(cmdDocsBytes), "]("+settings.CmdName+"_", "]("+path.Join(docsDir, settings.CmdName)+"_")

	// Replace the template marker
	finalReadme := strings.Replace(readme, templateMarker, cmdDocs, 1)

	// Write the final README
	err = os.WriteFile("README.md", []byte(finalReadme), 0644)
	if err != nil {
		fmt.Println("failed to write final README:", err)
		os.Exit(1)
	}
}
