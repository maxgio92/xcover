// Command pidfilter loops calling a per-instance marker function until the
// stop file given as the second argument appears, or until a deadline passes.
// The first argument selects the marker so an e2e test can tell from a
// coverage report which instance was traced.
package main

import (
	"fmt"
	"os"
	"time"
)

const deadline = 60 * time.Second

var sink int

//go:noinline
func tick() int { return 1 }

//go:noinline
func onlyA() int { return 2 }

//go:noinline
func onlyB() int { return 3 }

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: pidfilter a|b STOP_FILE")
		os.Exit(2)
	}
	var marker func() int
	switch os.Args[1] {
	case "a":
		marker = onlyA
	case "b":
		marker = onlyB
	default:
		fmt.Fprintf(os.Stderr, "unknown instance %q\n", os.Args[1])
		os.Exit(2)
	}
	stop := os.Args[2]

	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		sink += tick() + marker()
		if _, err := os.Stat(stop); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "pidfilter: deadline reached before stop file appeared")
	os.Exit(1)
}
