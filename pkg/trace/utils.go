package trace

import "unicode"

func clen(n []byte) int {
	for i := 0; i < len(n); i++ {
		if n[i] == 0 {
			return i
		}
	}
	return len(n)
}

// Remove null and non-printable characters
func cleanComm(byteSlice [16]byte) []byte {
	var cleaned []byte
	for _, b := range byteSlice {
		if b != 0 && unicode.IsPrint(rune(b)) {
			// Only append printable characters and not null bytes
			cleaned = append(cleaned, b)
		}
	}
	return cleaned
}
