//go:build integration

package trace

import (
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestDropElevatedCapabilities(t *testing.T) {
	err := dropElevatedCapabilities()
	require.NoError(t, err)

	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	err = unix.Capget(&hdr, &data[0])
	require.NoError(t, err)

	require.Zero(t, data[0].Effective&(uint32(1)<<unix.CAP_SYS_ADMIN),
		"CAP_SYS_ADMIN should be cleared from effective set")
	require.Zero(t, data[0].Permitted&(uint32(1)<<unix.CAP_SYS_ADMIN),
		"CAP_SYS_ADMIN should be cleared from permitted set")
	require.Zero(t, data[1].Effective&(uint32(1)<<(unix.CAP_BPF-32)),
		"CAP_BPF should be cleared from effective set")
	require.Zero(t, data[1].Permitted&(uint32(1)<<(unix.CAP_BPF-32)),
		"CAP_BPF should be cleared from permitted set")
}

func TestDropElevatedCapabilities_AllThreads(t *testing.T) {
	err := dropElevatedCapabilities()
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Pin this goroutine to its OS thread to ensure we're
			// checking a thread that didn't call dropElevatedCapabilities.
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()

			hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
			var data [2]unix.CapUserData
			err := unix.Capget(&hdr, &data[0])
			require.NoError(t, err)

			require.Zero(t, data[0].Effective&(uint32(1)<<unix.CAP_SYS_ADMIN),
				"CAP_SYS_ADMIN should be cleared on all threads")
			require.Zero(t, data[0].Permitted&(uint32(1)<<unix.CAP_SYS_ADMIN),
				"CAP_SYS_ADMIN should be cleared on all threads")
			require.Zero(t, data[1].Effective&(uint32(1)<<(unix.CAP_BPF-32)),
				"CAP_BPF should be cleared on all threads")
			require.Zero(t, data[1].Permitted&(uint32(1)<<(unix.CAP_BPF-32)),
				"CAP_BPF should be cleared on all threads")
		}()
	}
	wg.Wait()
}
