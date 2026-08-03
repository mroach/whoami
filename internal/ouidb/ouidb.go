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

var (
	once sync.Once
	db   map[uint32]string // oui to vendor name e.g. `258 => "3COM"`
)

// Given a MAC address, try to look-up the vendor based on the
// OUI (Organizationally-unique Identifier), the first 3 bytes of the address.
func Lookup(mac net.HardwareAddr) (vendor string, ok bool) {
	once.Do(load)
	oui := ouiFromMAC(mac)
	vendor, ok = db[oui]
	return
}

func ouiFromMAC(mac net.HardwareAddr) (oui uint32) {
	return uint32(mac[0])<<16 | uint32(mac[1])<<8 | uint32(mac[2])
}

func load() {
	r := csv.NewReader(strings.NewReader(ouiCSV))

	// skip the header
	if _, err := r.Read(); err != nil {
		panic(err)
	}

	db = make(map[uint32]string, 40000)

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

func parseOUI(s string) (oui uint32, ok bool) {
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 6 {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}
