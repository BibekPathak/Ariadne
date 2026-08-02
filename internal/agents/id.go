package agents

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	ts := time.Now().UTC().Format("20060102T150405")
	return ts + "_" + hex.EncodeToString(b)
}

func newRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
