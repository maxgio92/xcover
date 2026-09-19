// Command pidfilter loops calling a per-instance marker function until the
// stop file given as the second argument appears, or until a deadline passes.
// The first argument selects the marker so an e2e test can tell from a
// coverage report which instance was traced. A worker pinned to a second
// thread calls workerMarker in its own loop, so a --pid filter that matches
// one thread instead of the thread group misses it.
package main

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"
)

const deadline = 60 * time.Second

var (
	sink       int
	workerSink int
)

// init keeps the main goroutine on the main thread for the whole run, so the
// thread the worker locks itself to is a different one.
func init() {
	runtime.LockOSThread()
}

//go:noinline
func tick() int { return 1 }

//go:noinline
func onlyA() int { return 2 }

//go:noinline
func onlyB() int { return 3 }

//go:noinline
func workerMarker() int { return 4 }

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

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runtime.LockOSThread()
		for {
			workerSink += workerMarker()
			select {
			case <-done:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()

	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		sink += tick() + marker()
		if _, err := os.Stat(stop); err == nil {
			close(done)
			wg.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "pidfilter: deadline reached before stop file appeared")
	os.Exit(1)
}
