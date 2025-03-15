#define K_NUM_MAP_ENTRIES	65536
#define PERF_MAX_STACK_DEPTH	127

#define TASK_COMM_LEN	16

#define MAX_ARRAY_SIZE			(1 << 7) // on stack
#define MAX_PERCPU_ARRAY_SIZE		(1 << 15) // on heaps
#define HALF_PERCPU_ARRAY_SIZE		(MAX_PERCPU_ARRAY_SIZE >> 1)
#define LIMIT_PERCPU_ARRAY_SIZE(x)	((x) & (MAX_PERCPU_ARRAY_SIZE - 1))
#define LIMIT_HALF_PERCPU_ARRAY_SIZE(x)	((x) & (HALF_PERCPU_ARRAY_SIZE - 1))

#define MAX_PATH_SIZE		4096 // PATH_MAX from <linux/limits.h>
#define LIMIT_PATH_SIZE(x)	((x) & (MAX_PATH_SIZE - 1))
#define MAX_PATH_COMPONENTS 	20


typedef struct histogram_key {
	u32 pid;
	u32 kernel_stack_id;
	u32 user_stack_id;
} histogram_key_t;

typedef struct buffer {
	u8 data[MAX_PERCPU_ARRAY_SIZE];
} buffer_t;

