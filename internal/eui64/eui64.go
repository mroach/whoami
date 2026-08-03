package eui64

import (
	"net"
	"net/netip"
)

// Checks if the given IPv6 address was generated using the EUI-64 scheme which turns
// the interface's MAC address into an interface ID with two small modifications.
// If this is an EUI-64 address, returns the original MAC address
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

	// Undo the local bit flip that was applied when the MAC was converted to an interface ID
	mac[0] ^= 0x02

	return mac, true
}
