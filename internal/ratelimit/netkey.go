package ratelimit

import "net/netip"

// ipv6Prefix is the prefix length used to collapse IPv6 addresses for rate
// limiting. A single ISP customer is routinely allocated a /64, so keying on
// the full 128-bit address would give them 2^64 independent buckets. Collapsing
// to /64 treats their entire allocation as one client, matching how IPs are
// actually assigned.
const ipv6Prefix = 64

func NetKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
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
