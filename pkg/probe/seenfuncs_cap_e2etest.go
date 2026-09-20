//go:build e2etest

package probe

import (
	"os"
	"strconv"
)

// seenFuncsCapEnv names the variable the e2e suite sets to shrink the
// seen_funcs map below the traced function count and provoke the drops
// warning.
const seenFuncsCapEnv = "XCOVER_E2E_SEEN_FUNCS_MAX"

// seenFuncsCapOverride returns the cap from XCOVER_E2E_SEEN_FUNCS_MAX when it
// parses as a positive integer, else 0 (no override). Only binaries built
// with the e2etest tag compile this variant.
func seenFuncsCapOverride() int {
	n, err := strconv.Atoi(os.Getenv(seenFuncsCapEnv))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
