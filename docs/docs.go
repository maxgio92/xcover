//go:build docs

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/maxgio92/xcover/pkg/cmd"
	"github.com/maxgio92/xcover/pkg/cmd/options"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra/doc"
)

const docsDir = "docs"

// fileTemplateHeader is a format string with one %s for the page title,
// prepended to every generated page. Empty means no header; set it to
// something like "---\ntitle: %s\n---\n" for a YAML front matter. A var,
// not a const, so vet does not flag the Sprintf while it is empty.
var fileTemplateHeader = ""

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
	// Links are written relative to docs/, where every generated page,
	// the root command included, lives.
	linkHandler = func(filename string) string {
		return filename
	}
)

func main() {
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

	// The README is written from its template. The CLI reference used to be
	// substituted here; it now stays under docs/ and the README links to it.
	readmeBytes, err := os.ReadFile("README.md.tpl")
	if err != nil {
		fmt.Println("failed to read README template:", err)
		os.Exit(1)
	}
	err = os.WriteFile("README.md", readmeBytes, 0644)
	if err != nil {
		fmt.Println("failed to write final README:", err)
		os.Exit(1)
	}
}
