/*
 * Idle scenario target.
 *
 * target_func is attached by xcover but never called during the timed loop.
 * idle_func (not probed) is called N times instead. The measured per-call
 * time reflects the overhead on code that is not probed while a probe is
 * attached to a different function in the same binary.
 *
 * Expected result: idle ns/call ≈ baseline ns/call, demonstrating that
 * attaching uprobes does not slow down unprobed code paths.
 *
 * Output: one line containing the average ns/call.
 */
#include <stdio.h>
#include <time.h>

#define N 1000000

/* Probed by xcover, but not called during the timed loop. */
int __attribute__((noinline)) target_func(int a, int b)
{
	return a + b;
}

/* Not probed; this is the function whose latency we measure. */
int __attribute__((noinline)) idle_func(int a, int b)
{
	return a * b;
}

int main(void)
{
	struct timespec start, end;
	volatile int result = 0;

	clock_gettime(CLOCK_MONOTONIC, &start);
	for (long i = 0; i < N; i++)
		result += idle_func((int)i, (int)(i + 1));
	clock_gettime(CLOCK_MONOTONIC, &end);

	double ns = (double)(end.tv_sec  - start.tv_sec)  * 1e9
	           + (double)(end.tv_nsec - start.tv_nsec);
	printf("%.2f\n", ns / N);

	return result == 0;
}
