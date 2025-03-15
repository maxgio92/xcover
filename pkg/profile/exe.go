package profile

import (
	"fmt"
	"os"
	"unsafe"

	bpf "github.com/aquasecurity/libbpfgo"
	"github.com/pkg/errors"
)

// getExePath returns the path of the executable for the specified process ID.
// It checks the validity of the path shared in the binprm BPF hash map.
// It falls back to the proc filesystem.
func (p *Profiler) getExePath(binprmInfoMap *bpf.BPFMap, pid int32) (*string, error) {
	var path string
	v, err := binprmInfoMap.GetValue(unsafe.Pointer(&pid))
	if err != nil {
		p.logger.Debug().Err(err).Msg("error getting exe_path from binprm_info BPF map")
	} else {
		v = v[:clen(v)]
		path = string(v)

		_, err = os.Stat(path)
		if err == nil {
			p.logger.Debug().Str("path", path).Int("pid", p.pid).Msg("exe_path found from binprm_info BPF map")
			return &path, nil
		}
		p.logger.Debug().Err(err).Str("path", path).Msg("exe_path got from binprm_info BPF map not found")
	}

	// Fallback to procfs.
	path, err = os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return nil, errors.Wrap(err, "error getting exe_path from procfs")
	}
	if _, err = os.Stat(path); err != nil {
		return nil, errors.Wrap(err, "error getting exe_path from procfs")
	}
	p.logger.Debug().Str("path", path).Int("pid", p.pid).Msg("exe_path found from procfs")

	return &path, nil
}
