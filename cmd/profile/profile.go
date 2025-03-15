package profile

import (
	"errors"
	"fmt"

	log "github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/maxgio92/utrace/internal/commands/options"
	"github.com/maxgio92/utrace/pkg/dag"
	"github.com/maxgio92/utrace/pkg/profile"
)

type Options struct {
	pid          int
	outputFormat string
	*options.CommonOptions
}

func NewCommand(opts *options.CommonOptions) *cobra.Command {
	o := &Options{0, "", opts}

	cmd := &cobra.Command{
		Use:   "profile",
		Short: "profile executes a sampling profiling and returns as result the residency fraction per stack trace",
		RunE:  o.Run,
	}
	cmd.Flags().IntVar(&o.pid, "pid", 0, "the PID of the process")
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", "dot", "the format of output (dot, text)")
	cmd.MarkFlagRequired("pid")

	return cmd
}

func (o *Options) Run(_ *cobra.Command, _ []string) error {
	if o.Debug {
		o.Logger = o.Logger.Level(log.DebugLevel)
	}

	profiler := profile.NewProfiler(
		profile.WithPID(o.pid),
		profile.WithSamplingPeriodMillis(11),
		profile.WithProbeName("sample_stack_trace"),
		profile.WithProbe(o.Probe),
		profile.WithMapStackTraces("stack_traces"),
		profile.WithMapHistogram("histogram"),
		profile.WithLogger(o.Logger),
	)

	// Run profile.
	report, err := profiler.RunProfile(o.Ctx)
	if err != nil {
		return err
	}

	switch o.outputFormat {
	case "dot":
		err = o.printDOT(report)
	case "text":
		err = o.printText(report)
	default:
		err = o.printText(report)
	}
	if err != nil {
		return err
	}

	return nil
}

// printDOT prints a DOT representation of the profile DAG.
func (o *Options) printDOT(graph *dag.DAG) error {
	dot, err := graph.DOT()
	if err != nil {
		return err
	}
	fmt.Println(dot)

	return nil
}

// printDOT prints a text representation of the profile DAG.
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
