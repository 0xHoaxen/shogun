package hanko

import (
	"crypto/sha256"
	"encoding/binary"
)

// BodyDigest returns the SHA-256 of a message's subject and body, the value
// bound into body_sha256. Both parts are length-prefixed so moving text from
// the subject into the body changes the digest. fude and tsubame must both use
// this function.
func BodyDigest(subject, body string) []byte {
	h := sha256.New()
	for _, part := range []string{subject, body} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return h.Sum(nil)
}
