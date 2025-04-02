package output

import "fmt"

var (
	LoadingSymbols = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	count          = 0
)

func SpinWheel() {
	fmt.Printf("\r%s", LoadingSymbols[count%len(LoadingSymbols)])
	count++
}
