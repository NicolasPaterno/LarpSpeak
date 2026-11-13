package domain

import "bytes"

// ParseHeader reads "<callId> <userId> [anything]\n" at the start of an audio packet.
func ParseHeader(p []byte) (callID, userID string, ok bool) {
	line, _, found := bytes.Cut(p, []byte("\n"))
	f := bytes.Fields(line)
	if !found || len(f) < 2 {
		return "", "", false
	}
	return string(f[0]), string(f[1]), true
}
