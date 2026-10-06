package hanko

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"sort"
	"strings"
)

// BodyDigest returns the SHA-256 of a message's subject and body, the value
// bound into body_sha256. Both parts are length-prefixed so moving text from
// the subject into the body changes the digest. fude and tsubame must both use
// this function.
func BodyDigest(subject, body string) []byte {
	h := sha256.New()
	writeLengthPrefixed(h, subject)
	writeLengthPrefixed(h, body)
	return h.Sum(nil)
}

// RecipientDigest returns the SHA-256 of a recipient list, the value bound into
// rcpt_sha256. Addresses are lower-cased, de-duplicated and sorted, so order
// and case do not matter, and each is length-prefixed. fude and tsubame must
// both use this function.
func RecipientDigest(addrs []string) []byte {
	seen := make(map[string]struct{}, len(addrs))
	norm := make([]string, 0, len(addrs))
	for _, a := range addrs {
		a = strings.ToLower(strings.TrimSpace(a))
		if _, dup := seen[a]; dup || a == "" {
			continue
		}
		seen[a] = struct{}{}
		norm = append(norm, a)
	}
	sort.Strings(norm)
	h := sha256.New()
	for _, a := range norm {
		writeLengthPrefixed(h, a)
	}
	return h.Sum(nil)
}

func writeLengthPrefixed(h hash.Hash, s string) {
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], uint64(len(s)))
	h.Write(n[:])
	h.Write([]byte(s))
}
