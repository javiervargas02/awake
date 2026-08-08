package session

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// ID identifies one session.
//
// ADR-0002 asks for an identifier that is unique, sortable, and not
// guessable. It is also used as a log filename, so it must be safe on any
// filesystem.
//
// The shape is a fixed-width UTC timestamp followed by random characters:
//
//	20260807t140311z-k3m9x2q7r1
//
// Fixed width matters: it makes lexicographic order match chronological
// order, so `ls` on the session log directory lists sessions oldest-first
// without any tooling.
type ID string

// idAlphabet excludes vowels and easily-confused characters, so an ID is
// unlikely to spell anything and is hard to mistranscribe.
const idAlphabet = "0123456789bcdfghjkmnpqrstvwxz"

const idRandomLen = 10

// NewID generates an identifier for a session starting at the given instant.
//
// Randomness comes from crypto/rand rather than math/rand: session IDs appear
// in filenames, and predictable names invite a class of problem that costs
// nothing to avoid here.
func NewID(now time.Time) (ID, error) {
	buf := make([]byte, idRandomLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating session id: %w", err)
	}

	var suffix strings.Builder
	suffix.Grow(idRandomLen)
	for _, b := range buf {
		suffix.WriteByte(idAlphabet[int(b)%len(idAlphabet)])
	}

	return ID(fmt.Sprintf("%s-%s",
		now.UTC().Format("20060102t150405z"),
		suffix.String(),
	)), nil
}

func (id ID) String() string { return string(id) }

// Valid reports whether an ID has the expected shape. It is used when reading
// a session record written by another version, or by a user's text editor.
func (id ID) Valid() bool {
	s := string(id)
	if len(s) != len("20060102t150405z")+1+idRandomLen {
		return false
	}
	if s[len("20060102t150405z")] != '-' {
		return false
	}
	if _, err := time.Parse("20060102t150405z", s[:len("20060102t150405z")]); err != nil {
		return false
	}
	for _, r := range s[len("20060102t150405z")+1:] {
		if !strings.ContainsRune(idAlphabet, r) {
			return false
		}
	}
	return true
}
