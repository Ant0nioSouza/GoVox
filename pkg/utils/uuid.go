package utils

import (
	"crypto/rand"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
)

func NewUUIDv7() uuid.UUID {
	var u uuid.UUID

	now := time.Now()
	timestampMs := uint64(now.UnixMilli())

	binary.BigEndian.PutUint64(u[0:8], timestampMs<<16)

	_, err := rand.Read(u[6:])
	if err != nil {
		panic(err)
	}

	u[6] = (u[6] & 0x0F) | (7 << 4)
	u[8] = (u[8] & 0x3F) | 0x80

	return u
}

func ExtractTimestampFromUUIDv7(u uuid.UUID) time.Time {
	timestampMs := int64(binary.BigEndian.Uint64(u[0:8]) >> 16)
	return time.UnixMilli(timestampMs)
}

func IsUUIDv7(u uuid.UUID) bool {
	return (u[6] >> 4) == 7
}
