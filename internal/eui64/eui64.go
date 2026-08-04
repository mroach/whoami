package eui64

import (
	"net"
	"net/netip"
)

// EUI-64 (Extended Unique Identifier) is a method of generating the 64-bit interface ID
// portion of an IPv6 from the network interface's 48-bit MAC address.
// This method is deprecated and no longer used by most IPv6 stacks.
//
// It works by shoving 0xFFFE between the two 24-bit halves and flipping the 7th
// bit of the first byte.

// Checks if an IPv6 address was generated using EUI-64 and if so, returning
// the orignal interface MAC address.
func DetectEUI64(ip netip.Addr) (mac net.HardwareAddr, ok bool) {
	if !ip.Is6() {
		return nil, false
	}

	ip16 := ip.As16()

	// the last 64 bits of the address are the interface ID
	interfaceId := ip16[8:16]

	// if 0xFFFE is in the middle of the two 24-bit halves, this is probably EUI-64
	if !(interfaceId[3] == 0xFF && interfaceId[4] == 0xFE) {
		return nil, false
	}

	mac = make(net.HardwareAddr, 6)
	copy(mac[0:3], interfaceId[0:3])
	copy(mac[3:6], interfaceId[5:8])

	// Flip the 7th bit of the first byte. This is known as the "local bit"
	mac[0] ^= 0x02

	return mac, true
}
