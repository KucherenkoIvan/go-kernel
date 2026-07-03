package ddd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// IDGenerator is the port for generating entity identifiers. Use-cases take
// an IDGenerator instead of generating ids inline — this keeps them
// deterministic in tests.
type IDGenerator interface {
	NewID() string
}

// UUIDv7Generator is the production IDGenerator backed by NewEntityID.
type UUIDv7Generator struct{}

func (UUIDv7Generator) NewID() string { return NewEntityID() }

// NewEntityID generates a UUIDv7 (RFC 9562): a 48-bit unix-millisecond
// timestamp followed by random bits, so ids sort roughly by creation time.
// Ordering within the same millisecond is random (no monotonic counter).
func NewEntityID() string {
	var b [16]byte

	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)

	if _, err := rand.Read(b[6:]); err != nil {
		panic(fmt.Sprintf("ddd: crypto/rand is unavailable: %v", err))
	}

	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}
