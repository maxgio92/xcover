/*
 * Hit scenario target.
 *
 * target_func is called N times. After the first call its cookie is already
 * in the seen_funcs BPF map, so all subsequent uprobe firings take the fast
 * path (map lookup hit → early return). The measured per-call time reflects
 * steady-state hit overhead.
 *
 * Output: one line containing the average ns/call, suitable for parsing by
 * the Go benchmark driver.
 */
#include <stdio.h>
#include <time.h>

#define N 1000000

int __attribute__((noinline)) target_func(int a, int b)
{
	return a + b;
}

int main(void)
{
	struct timespec start, end;
	volatile int result = 0;

	clock_gettime(CLOCK_MONOTONIC, &start);
	for (long i = 0; i < N; i++)
		result += target_func((int)i, (int)(i + 1));
	clock_gettime(CLOCK_MONOTONIC, &end);

	double ns = (double)(end.tv_sec  - start.tv_sec)  * 1e9
	           + (double)(end.tv_nsec - start.tv_nsec);
	printf("%.2f\n", ns / N);

	return result == 0;
}
