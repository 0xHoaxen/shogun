// Package netguard keeps requests that carry a user-chosen address, such as a
// job source or an AI provider URL, away from this machine and the networks it
// sits on. A name is checked after DNS, at the moment of connecting, so a name
// that resolves inside the network is refused too, and so is every redirect.
package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrNotPublic means the address names this machine or a private network.
var ErrNotPublic = errors.New("netguard: address is not public")

const (
	dialTimeout   = 10 * time.Second
	headerTimeout = 30 * time.Second
)

// cgnat is the shared address space of carrier-grade NAT, which is not public
// but is not covered by netip's IsPrivate.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsPublic reports whether addr is an address on the public internet: not
// loopback, private, link-local, multicast, unspecified or shared. An IPv4
// address written as IPv6 is judged as the IPv4 address it is.
func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() && !addr.IsMulticast() && !addr.IsUnspecified() && !cgnat.Contains(addr)
}

// DialControl is a net.Dialer Control function that refuses any address that is
// not public. It runs once DNS has resolved, so it sees the address actually
// being connected to.
func DialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ErrNotPublic
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !IsPublic(ip) {
		return ErrNotPublic
	}
	return nil
}

// Transport returns an HTTP transport that only connects to public addresses
// and ignores proxy settings from the environment, which would otherwise let a
// request leave by another route.
func Transport() *http.Transport {
	dialer := &net.Dialer{Timeout: dialTimeout, Control: DialControl}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: headerTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       time.Minute,
	}
}
