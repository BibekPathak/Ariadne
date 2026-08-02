package sandbox

import (
	"crypto/rand"
	"encoding/hex"
)

func randomID(n int) string {
	b := make([]byte, n/2+1)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}
