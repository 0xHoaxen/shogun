package gmail

import (
	netmail "net/mail"
	"strings"
)

// firstAddress returns the first address of a header value, lower-cased.
func firstAddress(header string) string {
	if all := addresses(header); len(all) > 0 {
		return all[0]
	}
	return ""
}

// addresses returns the bare, lower-cased addresses of a header value. A value
// that does not parse is kept whole rather than dropped, so mail from an odd
// sender is still recorded.
func addresses(header string) []string {
	header = strings.TrimSpace(header)
	if header == "" {
		return nil
	}
	parsed, err := netmail.ParseAddressList(header)
	if err != nil {
		return []string{strings.ToLower(header)}
	}
	out := make([]string, len(parsed))
	for i, a := range parsed {
		out[i] = strings.ToLower(a.Address)
	}
	return out
}
