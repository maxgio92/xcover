// gen generates a C source file with N distinct noinline functions, one per
// uprobe cookie. Used for the miss benchmark scenario: each function is called
// exactly once, so every uprobe firing hits the full slow path in the BPF
// program (cookie not in seen_funcs → map update → ringbuf reserve → submit).
//
// Usage:
//
//	go run ./target/miss/gen -n 1000 > target/miss/miss.c
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	n := flag.Int("n", 1000, "number of distinct functions to generate")
	flag.Parse()

	w := os.Stdout

	fmt.Fprintln(w, "#include <stdio.h>")
	fmt.Fprintln(w, "#include <time.h>")
	fmt.Fprintln(w)

	for i := 0; i < *n; i++ {
		fmt.Fprintf(w, "int __attribute__((noinline)) func_%d(int a) { return a + %d; }\n", i, i)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "typedef int (*fn_t)(int);")
	fmt.Fprint(w, "static fn_t funcs[] = {")
	for i := 0; i < *n; i++ {
		if i > 0 {
			fmt.Fprint(w, ", ")
		}
		fmt.Fprintf(w, "func_%d", i)
	}
	fmt.Fprintln(w, "};")
	fmt.Fprintln(w)

	fmt.Fprintf(w, `int main(void)
{
	struct timespec start, end;
	volatile int result = 0;
	int n = %d;

	clock_gettime(CLOCK_MONOTONIC, &start);
	for (int i = 0; i < n; i++)
		result += funcs[i](i);
	clock_gettime(CLOCK_MONOTONIC, &end);

	double ns = (double)(end.tv_sec  - start.tv_sec)  * 1e9
	           + (double)(end.tv_nsec - start.tv_nsec);
	printf("%%.2f\n", ns / n);

	return result == 0;
}
`, *n)
}
