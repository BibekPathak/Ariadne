package memory

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewID returns a time-ordered random id.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102T150405Z") + "_" + hex.EncodeToString(b)
}
