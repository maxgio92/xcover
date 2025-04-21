package output

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

func PrintRight(text string) {
	// Get terminal width.
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		width = 80
	}

	// Set padding.
	padding := width - len(text)
	if padding < 0 {
		padding = 0
	}

	fmt.Printf("\r%s%s", spaces(padding), text)
}

func spaces(n int) string {
	return fmt.Sprintf("%*s", n, "")
}
