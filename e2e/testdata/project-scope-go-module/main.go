package main

import (
	"fmt"

	"example.com/testmod/pkg"
)

//go:noinline
func appLogic() int { return 42 }

func main() {
	fmt.Println(appLogic(), pkg.Helper())
}
