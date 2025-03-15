#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include "profile.bpf.h"
#include <string.h>

struct {
	__uint(type, BPF_MAP_TYPE_STACK_TRACE);
	__uint(key_size, sizeof(u32));
	__uint(value_size, PERF_MAX_STACK_DEPTH * sizeof(u64));
	__uint(max_entries, K_NUM_MAP_ENTRIES);
} stack_traces SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, histogram_key_t);		/* per-process stack trace key */
	__type(value, u64);			/* sample count */
	__uint(max_entries, K_NUM_MAP_ENTRIES);
} histogram SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, u32);			/* pid */
	__type(value, char[MAX_ARRAY_SIZE]);	/* exe_path */
	__uint(max_entries, K_NUM_MAP_ENTRIES);
} binprm_info SEC(".maps");

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	__type(key, u32);
	__type(value, buffer_t);
	__uint(max_entries, 1);
} heaps SEC(".maps");

/*
 * get_pathname_from_path lookups pathname from path struct
 * Thanks to tracee: https://github.com/aquasecurity/tracee/blob/a6118678c6908c74d6ee26ca9183e99932d098c9/pkg/ebpf/c/common/filesystem.h#L160
*/
static __always_inline long get_pathname_from_path(struct path *path, buffer_t *out_buf)
{
	struct dentry *dentry, *dentry_parent, *dentry_mnt_root;
	struct vfsmount *vfsmnt;
	struct mount *mnt, *mnt_parent;
	const u_char *dentry_name;
	size_t dentry_name_len;

	char slash = '/';
	int zero = 0;

	dentry = BPF_CORE_READ(path, dentry); /* Directory entry of the specified path */
	vfsmnt = BPF_CORE_READ(path, mnt); /* VFS mount of the specified path */
	mnt = container_of(vfsmnt, struct mount, mnt); /* Mount struct of the VFS mount */
	mnt_parent = BPF_CORE_READ(mnt, mnt_parent); /* Parent mount of the VFS mount, if not global root */

	unsigned int buf_off = HALF_PERCPU_ARRAY_SIZE;
	unsigned int dentry_name_off;

	int sz;

#pragma unroll
	for (int i = 0; i < MAX_PATH_COMPONENTS; i++) {
		dentry_mnt_root = BPF_CORE_READ(vfsmnt, mnt_root);
		dentry_parent = BPF_CORE_READ(dentry, d_parent);

		/* We reached root */
		if (dentry == dentry_mnt_root || dentry == dentry_parent) {
			if (dentry != dentry_mnt_root) {
				/* Not mount root */
				break;
			}
			if (mnt != mnt_parent) {
				/* Not global root - continue with mount point path */
				dentry = BPF_CORE_READ(mnt, mnt_mountpoint);
				mnt = BPF_CORE_READ(mnt, mnt_parent);
				mnt_parent = BPF_CORE_READ(mnt, mnt_parent);
				bpf_core_read(&vfsmnt, sizeof(struct vfsmnt *), &mnt->mnt);
				continue;
			}
			/* Global root - path fully parsed */
			break;
		}

		/* Add this dentry name to path */
		dentry_name = BPF_CORE_READ(dentry, d_name.name); /* directory name as quick string (qstr) */
		dentry_name_len = LIMIT_PATH_SIZE(BPF_CORE_READ(dentry, d_name.len) + 1); /* Add slash (1) */
		dentry_name_off = buf_off - dentry_name_len;
		/* Is string buffer big enough for dentry name? */
		if (dentry_name_off > buf_off) { /* Wrap around */
			break;
		}
		/* Copy the directory name to the output buffer */
		sz = bpf_probe_read_kernel_str(
			&(out_buf->data[LIMIT_HALF_PERCPU_ARRAY_SIZE(dentry_name_off)]), dentry_name_len, dentry_name);
		if (sz > 1) {
			buf_off -= 1; /* Remove null byte termination with slash sign */
			bpf_probe_read_kernel(&(out_buf->data[LIMIT_HALF_PERCPU_ARRAY_SIZE(buf_off)]), 1,  &slash);
			buf_off -= sz - 1;
		} else {
			/*
			 * If copied size is 0 or 1 we have an error (path can't be null nor an empty string)
			 * The same, if the returned size is negative an error occurred
			*/
			break;
		}

		/* Go one level up */
		dentry = dentry_parent;
	}
	if (buf_off == HALF_PERCPU_ARRAY_SIZE) {
		/* memfd files have no path in the filesystem -> extract their name */
		buf_off = 0;
		dentry_name = BPF_CORE_READ(dentry, d_name.name);
		bpf_probe_read_kernel(&(out_buf->data[0]), MAX_PATH_SIZE, dentry_name);
	} else {
		/* Add leading slash */
		buf_off -= 1;
		bpf_probe_read_kernel(&(out_buf->data[LIMIT_HALF_PERCPU_ARRAY_SIZE(buf_off)]), 1, &slash);
		/* Null terminate the path string */
		bpf_probe_read_kernel(&(out_buf->data[HALF_PERCPU_ARRAY_SIZE - 1]), 1, &zero);
	}
	return buf_off;
}

/* get_buffer takes a buffer from per-CPU array map */
static __always_inline buffer_t *get_buffer(int idx)
{
	return (buffer_t *)bpf_map_lookup_elem(&heaps, &idx);
}

/*
 * get_task_exe_pathname returns the task exe_file pathname.
 * This does not apply to kernel threads as they share the same memory-mapped address space,
 * as opposed to user address space.
 */
static __always_inline void *get_task_exe_pathname(struct task_struct *task)
{
	/*
	 * Get ref file path from the task's user space memory mapping descriptor.
	 * exe_file->f_path could also be accessed from current task's binprm struct 
	 * (ctx->args[2]->file->f_path)
	 */
	struct path path = BPF_CORE_READ(task, mm, exe_file, f_path);

	buffer_t *string_buf = get_buffer(0);
	if (string_buf == NULL) {
		return NULL;
	}
	/* Write path string from path struct to the buffer */
	size_t buf_off = get_pathname_from_path(&path, string_buf);
	return &string_buf->data[buf_off];
}

SEC("perf_event")
int sample_stack_trace(struct bpf_perf_event_data* ctx)
{
	char hist_insert_fmt[] = "stack trace histogram insert pid=%d comm=%s exe_path=%s\n";
	histogram_key_t key;
	struct bpf_perf_event_value value_buf;
	u64 *count, one = 1;
	char comm[TASK_COMM_LEN];

	struct task_struct *task; 
	char *exe_path;
	char exe_path_str[MAX_ARRAY_SIZE];
	int len = 0;

	/* Get current task executable pathname */
	task = (struct task_struct *)bpf_get_current_task(); /* Current task struct */
	exe_path = get_task_exe_pathname(task);
	if (exe_path == NULL) {
		return 0;
	}
	len = bpf_core_read_str(&exe_path_str, sizeof(exe_path_str), exe_path);
	if (len < 0) {
		return 0;
	}

	/* Sample the user and kernel stack traces, and record in the stack_traces structure. */
	key.pid = bpf_get_current_pid_tgid() >> 32;
	key.kernel_stack_id = bpf_get_stackid(ctx, &stack_traces, 0 | BPF_F_FAST_STACK_CMP);
	key.user_stack_id = bpf_get_stackid(ctx, &stack_traces, 0 | BPF_F_FAST_STACK_CMP | BPF_F_USER_STACK);
	if ((int)key.kernel_stack_id < 0 && (int)key.user_stack_id < 0) {
		return 0;
	}

	/* Get current task command */
	bpf_get_current_comm(&comm, sizeof(comm));

	/* Upsert stack trace histogram and binprm_info */
	count = (u64*)bpf_map_lookup_elem(&histogram, &key);
	if (count) {
		(*count)++;
	} else {
		bpf_map_update_elem(&histogram, &key, &one, BPF_NOEXIST);
		bpf_map_update_elem(&binprm_info, &key.pid, &exe_path_str, BPF_ANY);
		bpf_trace_printk(hist_insert_fmt, sizeof(hist_insert_fmt), key.pid, comm, exe_path_str);
	}

	return 0;
}

char _license[] SEC("license") = "GPL";

