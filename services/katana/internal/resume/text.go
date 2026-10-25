package resume

import (
	"bytes"
	"unicode/utf16"
	"unicode/utf8"
)

// readText decodes a plain text file. It takes UTF-8, with or without a byte
// order mark, and UTF-16 with one; anything else, including any file with a NUL
// byte in it, is not text.
func readText(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, bomUTF8):
		data = data[len(bomUTF8):]
	case bytes.HasPrefix(data, bomUTF16L):
		return decodeUTF16(data[len(bomUTF16L):], true)
	case bytes.HasPrefix(data, bomUTF16B):
		return decodeUTF16(data[len(bomUTF16B):], false)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", fail(ReasonUnsupported, "That does not look like a PDF, Word or text file.")
	}
	return string(data), nil
}

func decodeUTF16(data []byte, littleEndian bool) (string, error) {
	if len(data)%2 != 0 {
		return "", fail(ReasonUnreadable, "That text file could not be read.")
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		a, b := uint16(data[2*i]), uint16(data[2*i+1])
		if littleEndian {
			units[i] = a | b<<8
		} else {
			units[i] = b | a<<8
		}
	}
	return string(utf16.Decode(units)), nil
}
