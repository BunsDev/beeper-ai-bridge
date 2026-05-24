package session

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"time"
)

func UUIDv7() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	millis := uint64(time.Now().UnixMilli())
	b[0] = byte(millis >> 40)
	b[1] = byte(millis >> 32)
	b[2] = byte(millis >> 24)
	b[3] = byte(millis >> 16)
	b[4] = byte(millis >> 8)
	b[5] = byte(millis)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b)
}

func CreateSessionID() string {
	return UUIDv7()
}

func CreateTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func formatUUID(b [16]byte) string {
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}

func uuidTimePrefix(id string) uint64 {
	if len(id) < 8 {
		return 0
	}
	var prefix [4]byte
	if _, err := hex.Decode(prefix[:], []byte(id[:8])); err != nil {
		return 0
	}
	return uint64(binary.BigEndian.Uint32(prefix[:]))
}
