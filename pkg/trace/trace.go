package trace

import (
	"C"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unsafe"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"

	"github.com/maxgio92/utrace/pkg/dag"
	"github.com/maxgio92/utrace/pkg/symtable"
)

func NewProfiler(opts ...ProfileOption) *Profiler {
	profile := new(Profiler)
	for _, f := range opts {
		f(profile)
	}
	profile.symTabELF = symtable.NewELFSymTab()

	return profile
}

func (p *Profiler) RunProfile(ctx context.Context) (*dag.DAG, error) {
	bpf.SetLoggerCbs(bpf.Callbacks{
		Log: func(level int, msg string) {
			return
		},
	})

	bpfModule, err := bpf.NewModuleFromBuffer(p.probe, p.probeName)
	if err != nil {
		return nil, errors.Wrap(err, "error creating the BPF module object")
	}
	defer bpfModule.Close()
	p.logger.Debug().Msg("loading BPF object")

	if err := bpfModule.BPFLoadObject(); err != nil {
		return nil, errors.Wrap(err, "error loading the BPF program")
	}
	p.logger.Debug().Msg("getting the loaded BPF program")

	prog, err := bpfModule.GetProgram(p.probeName)
	if err != nil {
		return nil, errors.Wrap(err, "error getting the BPF program object")
	}
	p.logger.Debug().Msg("attaching the BPF program sampler")

	if err = p.attachSampler(prog); err != nil {
		return nil, errors.Wrap(err, "error attaching the sampler")
	}
	p.logger.Debug().Msg("collecting data")
	p.logger.Debug().Msg("getting the stack traces BPF map")

	stackTracesMap, err := bpfModule.GetMap(p.mapStackTraces)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("error getting %s BPF map", p.mapStackTraces))
	}
	p.logger.Debug().Msg("getting the stack trace counts (histogramMap) BPF maps")

	histogramMap, err := bpfModule.GetMap(p.mapHistogram)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("error getting %s BPF map", p.mapHistogram))
	}

	binprmInfo, err := bpfModule.GetMap("binprm_info")
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("error getting %s BPF map", "binprm_info"))
	}

	// Iterate over the stack trace counts histogramMap map.
	counts := make(map[string]int, 0)
	traces := make(map[string][]string, 0)
	totalCount := 0

	p.logger.Debug().Msg("iterating over the retrieved histogramMap items")

	// Try to load symbols.

	// For each function being sampled.
	traceWG, ctx := errgroup.WithContext(ctx)
	tracesCh := make(chan []string, 0)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	traceWG.Go(func() error {
		defer close(tracesCh)
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			default:
				if it := histogramMap.Iterator(); it.Next() {
					k := it.Key()

					// Get count for the specific sampled stack trace.
					v, err := histogramMap.GetValue(unsafe.Pointer(&k[0]))
					if err != nil {
						return errors.Wrap(err, fmt.Sprintf("error getting stack trace count for key %v", k))
					}
					count := int(binary.LittleEndian.Uint64(v))

					var key HistogramKey
					if err = binary.Read(bytes.NewBuffer(k), binary.LittleEndian, &key); err != nil {
						return errors.Wrap(err, fmt.Sprintf("error reading the stack trace count key %v", k))
					}

					// When filtering by PID, skip unwanted tasks.
					if p.pid > 0 && int(key.Pid) != p.pid {
						continue
					}

					// When filtering by comm, skip unwanted tasks.
					comm := string(cleanComm(key.Comm))
					if p.comm != "" && !strings.Contains(comm, p.comm) {
						continue
					}

					p.logger.Debug().Int("pid", int(key.Pid)).Str("comm", comm).Uint32("user_stack_id", key.UserStackId).Uint32("kernel_stack_id", key.KernelStackId).Int("count", count).Msg("got stack traces")

					// symbols contains the symbols list for current trace of the kernel and user stacks.
					symbols := make([]string, 0)

					// Append symbols from user stack.
					if int32(key.UserStackId) >= 0 {
						stackTrace, err := p.getStackTraceByID(stackTracesMap, key.UserStackId)
						if err != nil {
							p.logger.Err(err).Int("pid", int(key.Pid)).Str("comm", comm).Uint32("id", key.UserStackId).Msg("error getting user stack trace")
							return errors.Wrap(err, "error getting user stack")
						}

						// Load the symbol table.
						err = p.loadSymTable(binprmInfo, key.Pid)
						if err != nil {
							p.logger.Debug().Int("pid", int(key.Pid)).Str("comm", comm).Uint32("id", key.UserStackId).Err(err).Msg("error loading the symbol table")
						}

						readableStackTrace, err := p.getHumanReadableStackTrace(stackTrace)
						if err != nil {
							p.logger.Debug().Int("pid", int(key.Pid)).Str("comm", comm).Uint32("id", key.UserStackId).Err(err).Msg("error resolving symbols")
						}

						symbols = append(symbols, readableStackTrace...)
						p.logger.Debug().Int("pid", int(key.Pid)).Str("comm", comm).Strs("trace", symbols).Msg("produced one trace")
					}

					// Ignore kernel stack trace.

					// Build a key for the histogram based on concatenated symbols.
					var symbolsKey string
					for _, symbol := range symbols {
						symbolsKey += fmt.Sprintf("%s;", symbol)
					}

					// Update the statistics.
					totalCount += count
					counts[symbolsKey] += count
					traces[symbolsKey] = symbols
					tracesCh <- symbols
				}
			}
		}
	})

	// Consume traces.
	symUniq := make(map[string]struct{})
	p.logger.Debug().Msg("consuming traces")
	for trace := range tracesCh {
		for _, v := range trace {
			if _, ok := symUniq[v]; !ok {
				fmt.Println(v)
				symUniq[v] = struct{}{}
			}
		}
		p.logger.Debug().Strs("trace", trace).Msg("consumed one trace")
	}

	// Check whether any of the goroutines failed. Since g is accumulating the
	// errors, we don't need to send them (or check for them) in the individual
	// results sent on the channel.
	if err := traceWG.Wait(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (p *Profiler) loadSymTable(binprmInfo *bpf.BPFMap, pid int32) error {
	// Get process executable path on filesystem.
	exePath, err := p.getExePath(binprmInfo, pid)
	if err != nil {
		p.logger.Debug().Int32("pid", pid).Msg("error getting executable path for symbolization")
		return err
	}
	p.logger.Debug().Str("path", *exePath).Int32("pid", pid).Msg("executable path found")

	// Try to load ELF symbol table, if it's an ELF executable.
	if err = p.symTabELF.Load(*exePath); err != nil {
		p.logger.Debug().Err(err).Msg("error loading the ELF symbol table")
		return err
	}
	p.logger.Debug().Str("path", *exePath).Int32("pid", pid).Msg("ELF symbol table loaded")

	return nil
}

// getStackTraceByID returns a StackTrace struct from the BPF_MAP_TYPE_STACK_TRACE map,
// keyed by stack ID returned by the get_stackid BPF helper.
func (p *Profiler) getStackTraceByID(stackTraces *bpf.BPFMap, stackID uint32) (*StackTrace, error) {
	v, err := stackTraces.GetValue(unsafe.Pointer(&stackID))
	if err != nil {
		return nil, err
	}

	var stackTrace StackTrace
	err = binary.Read(bytes.NewBuffer(v), binary.LittleEndian, &stackTrace)
	if err != nil {
		return nil, err
	}

	return &stackTrace, nil
}

// getHumanReadableStackTrace returns a string containing the resolved symbols separated by ';'
// for the process of the ID that is passed as argument.
// Symbolization is supported for non-stripped ELF executable binaries, because the .symtab
// ELF section is looked up.
func (p *Profiler) getHumanReadableStackTrace(stackTrace *StackTrace) ([]string, error) {
	symbols := make([]string, 0)

	for _, ip := range stackTrace {
		if ip == 0 {
			continue
		}
		symbol, err := p.symTabELF.GetName(ip, false)
		if err != nil {
			return nil, err
		}

		if symbol == "" {
			// Fallback to hex instruction pointer address.
			symbol = fmt.Sprintf("%#016x", ip)
		}
		symbols = append(symbols, symbol)
	}

	return symbols, nil
}
