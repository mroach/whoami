package ouidb

import (
	_ "embed"
	"encoding/csv"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

//go:embed oui.csv
var ouiCSV string

// Organizationally-Unique Identifier
// IEEE-assigned 24-bit number to uniquely identify hardware vendors.
// Typically rendered in 3 hex octets e.g. 08:00:2B, we store them internally
// as integers for fast lookup 08:00:2B => 524331
type OUI = uint32

var (
	once sync.Once
	db   map[OUI]string // oui to vendor name e.g. `258 => "3COM"`
)

// Given a MAC address, try to look-up the vendor based on the
// OUI (Organizationally-unique Identifier): the first 3 bytes of the address.
func Lookup(mac net.HardwareAddr) (vendor string, ok bool) {
	once.Do(load)
	oui := ouiFromMAC(mac)
	vendor, ok = db[oui]
	return
}

func ouiFromMAC(mac net.HardwareAddr) (oui OUI) {
	return OUI(mac[0])<<16 | OUI(mac[1])<<8 | OUI(mac[2])
}

func load() {
	r := csv.NewReader(strings.NewReader(ouiCSV))

	// skip the header
	if _, err := r.Read(); err != nil {
		panic(err)
	}

	db = make(map[OUI]string, 40000)

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		oui, ok := parseOUI(rec[0])
		if !ok {
			continue
		}

		db[oui] = rec[1]
	}
}

func parseOUI(s string) (oui OUI, ok bool) {
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 6 {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return OUI(n), true
}
