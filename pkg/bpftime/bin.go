package bpftime

import _ "embed"

// bptimeBin is the embedded bpftime CLI binary, used for Frida-based agent
// injection via 'bpftime attach <pid>'. It is populated by 'make bpftime-libs'
// which copies the built binary to pkg/bpftime/bin/bpftime.
//
//go:embed bin/bpftime
var bptimeBin []byte

// BptimeBinBytes returns the raw bytes of the embedded bpftime CLI binary.
func BptimeBinBytes() []byte {
	b := make([]byte, len(bptimeBin))
	copy(b, bptimeBin)
	return b
}
