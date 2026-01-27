package util

import (
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropy     = ulid.Monotonic(rand.Reader, 0)
	entropyLock sync.Mutex
)

// NewULID generates a new ULID string.
// ULIDs are time-sortable unique identifiers.
func NewULID() string {
	entropyLock.Lock()
	defer entropyLock.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String()
}

// NewULIDWithTime generates a ULID for a specific time.
// Useful for importing commits with preserved timestamps.
func NewULIDWithTime(t time.Time) string {
	entropyLock.Lock()
	defer entropyLock.Unlock()
	return ulid.MustNew(ulid.Timestamp(t), entropy).String()
}

// ParseULID parses a ULID string and returns its timestamp.
func ParseULID(s string) (time.Time, error) {
	id, err := ulid.Parse(s)
	if err != nil {
		return time.Time{}, err
	}
	return ulid.Time(id.Time()), nil
}

// ValidateULID checks if a string is a valid ULID.
func ValidateULID(s string) bool {
	_, err := ulid.Parse(s)
	return err == nil
}

// ShortID returns the last 7 characters of an ID in lowercase.
// For ULIDs, the last part has more entropy than the first (timestamp) part.
// Lowercase matches git's convention for short hashes.
func ShortID(id string) string {
	if len(id) <= 7 {
		return strings.ToLower(id)
	}
	return strings.ToLower(id[len(id)-7:])
}
