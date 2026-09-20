//go:build !e2etest

package probe

// seenFuncsCapOverride returns 0: release builds size seen_funcs from the
// traced function count alone. The e2etest build tag replaces this with a
// variant that reads XCOVER_E2E_SEEN_FUNCS_MAX.
func seenFuncsCapOverride() int {
	return 0
}
