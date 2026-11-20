// Package ulidx generates ULIDs (specs/02.5, specs/09.3).
package ulidx

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// New returns a fresh ULID string.
func New() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}

// NewFromTime returns a ULID anchored at t (used by seeds/tests for stable ordering).
func NewFromTime(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), rand.Reader).String()
}
