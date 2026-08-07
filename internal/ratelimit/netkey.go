package ratelimit

import "net/netip"

// ipv6Prefix is the prefix length IPv6 clients are grouped by. A /64 is the
// smallest block a customer is normally assigned; many get something larger.
const ipv6Prefix = 64

// NetKey reduces a client address to the network whose budget it shares.
//
// This is what makes a per-address limit mean anything. An IPv4 address is its
// own network, but IPv6 is not: one residential customer is routinely handed a
// whole /64, so keying on the full address would hand a fresh budget to each of
// 18 quintillion source addresses and the limit would never bind.
//
// IPv4-mapped addresses are unmapped first, so ::ffff:198.51.100.7 and
// 198.51.100.7 share one key rather than getting one each. An address that will
// not parse is returned unchanged: limiting an unrecognised client too strictly
// beats not limiting it at all.
func NetKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	// A zone (%eth0) is not part of the identity for limiting purposes.
	addr = addr.WithZone("").Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(ipv6Prefix)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}
