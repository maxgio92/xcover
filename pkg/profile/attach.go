package profile

import (
	"runtime"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func (p *Profiler) attachSampler(prog *bpf.BPFProg) error {
	for i := 0; i < runtime.NumCPU(); i++ {
		// The perf event attribute set.
		attr := &unix.PerfEventAttr{

			// If type is PERF_TYPE_SOFTWARE, we are measuring software events provided by the kernel.
			Type: unix.PERF_TYPE_SOFTWARE,

			// This reports the CPU clock, a high-resolution per-CPU timer.
			Config: unix.PERF_COUNT_SW_CPU_CLOCK,

			// A "sampling" event is one that generates an overflow notification every N events,
			// where N is given by sample_period.
			// sample_freq can be used if you wish to use frequency rather than period.
			// sample_period and sample_freq are mutually exclusive.
			// The kernel will adjust the sampling period to try and achieve the desired rate.
			Sample: p.samplingPeriodMillis * 1000 * 1000,
		}
		p.logger.Debug().Msg("opening the sampling software cpu block perf event")

		// Create the perf event file descriptor that corresponds to one event that is measured.
		// We're measuring a clock timer software event just to run the program on a periodic schedule.
		// When a specified number of clock samples occur, the kernel will trigger the program.
		evt, err := unix.PerfEventOpen(
			// The attribute set.
			attr,

			// the specified task.
			-1,

			// on the Nth CPU.
			i,

			// The group_fd argument allows event groups to be created. An event group has one event which
			// is the group leader. A single event on its own is created with group_fd = -1 and is considered
			// to be a group with only 1 member.
			-1,

			// The flags.
			0,
		)
		if err != nil {
			return errors.Wrap(err, "error creating the perf event")
		}
		defer func() {
			if err := unix.Close(evt); err != nil {
				p.logger.Fatal().Err(err).Msg("failed to close perf event")
			}
		}()
		p.logger.Debug().Msgf("attaching the BPF program to the sampling perf event for cpu #%d", i)

		// Attach the BPF program to the sampling perf event.
		if _, err = prog.AttachPerfEvent(evt); err != nil {
			return errors.Wrap(err, "error attaching the BPF program to the sampling perf event")
		}
	}
	return nil
}
