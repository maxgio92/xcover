package main

import (
	"fmt"
	"os"
)

//go:noinline
func add(a, b int) int {
	return a + b
}

//go:noinline
func multiply(a, b int) int {
	return a * b
}

//go:noinline
func subtract(a, b int) int {
	return a - b
}

//go:noinline
func divide(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

//go:noinline
func greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: demo-app <command>")
		fmt.Println("Commands: add, multiply, greet")
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "add":
		result := add(10, 20)
		fmt.Printf("10 + 20 = %d\n", result)
	case "multiply":
		result := multiply(5, 6)
		fmt.Printf("5 * 6 = %d\n", result)
	case "greet":
		greet("xcover")
	case "all":
		// Call all functions for maximum coverage
		fmt.Println("Running all functions:")
		fmt.Printf("Add: %d\n", add(10, 20))
		fmt.Printf("Multiply: %d\n", multiply(5, 6))
		fmt.Printf("Subtract: %d\n", subtract(30, 10))
		fmt.Printf("Divide: %d\n", divide(100, 5))
		greet("xcover")
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
	}
}
