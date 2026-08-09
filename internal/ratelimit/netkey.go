package ratelimit

import "net/netip"

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
