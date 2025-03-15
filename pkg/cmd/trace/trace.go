package trace

import (
	"errors"
	"fmt"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/utrace/internal/commands/options"
	"github.com/maxgio92/utrace/pkg/dag"
	"github.com/maxgio92/utrace/pkg/trace"
)

type Options struct {
	pid          int
	comm         string
	outputFormat string
	*options.CommonOptions
}

func NewCommand(opts *options.CommonOptions) *cobra.Command {
	o := &Options{0, "", "", opts}

	cmd := &cobra.Command{
		Use:   "trace",
		Short: "trace executes a sampling profiler and returns the userspace functions run by the selected processes",
		RunE:  o.Run,
	}
	cmd.Flags().IntVar(&o.pid, "pid", 0, "Filter the process by PID")
	cmd.Flags().StringVarP(&o.comm, "comm", "c", "", "Filter the processes by command")
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", "dot", "the format of output (dot, text)")

	return cmd
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	if o.Debug {
		o.Logger = o.Logger.Level(log.DebugLevel)
	}

	profiler := trace.NewProfiler(
		trace.WithPID(o.pid),
		trace.WithComm(o.comm),
		trace.WithSamplingPeriodMillis(11),
		trace.WithProbeName("sample_stack_trace"),
		trace.WithProbe(o.Probe),
		trace.WithMapStackTraces("stack_traces"),
		trace.WithMapHistogram("histogram"),
		trace.WithLogger(o.Logger),
	)

	// Run trace.
	_, err := profiler.RunProfile(o.Ctx)
	if err != nil {
		return err
	}

	return nil
}

// printDOT prints a DOT representation of the trace DAG.
func (o *Options) printDOT(graph *dag.DAG) error {
	dot, err := graph.DOT()
	if err != nil {
		return err
	}
	fmt.Println(dot)

	return nil
}

// printDOT prints a text representation of the trace DAG.
func (o *Options) printText(graph *dag.DAG) error {
	it := graph.Nodes()
	for it.Next() {
		n := it.Node()
		if n == nil {
			return errors.New("node is nil")
		}

		v := graph.Node(n.ID())
		node, ok := v.(*dag.Node)
		if !ok {
			return fmt.Errorf("unexpected node type: %T", node)
		}
		if node.Weight > 0 {
			fmt.Printf("%.1f%%	%s\n", node.Weight*100, node.Symbol)
		}
	}

	return nil
}
