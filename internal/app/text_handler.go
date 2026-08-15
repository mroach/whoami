package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mroach/whoami/internal/version"
)

func (app *App) TextHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	app.logHit(rd)

	w.Header().Add("content-type", "text/plain")

	ipString := rd.IP.Address
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", strings.Count(ipString, "")-1))
	fmt.Fprintf(w, "%s\n", ipString)
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("-", strings.Count(ipString, "")-1))

	if loc := rd.Location; loc != nil {
		fmt.Fprintf(w, "%-20s %s\n", "Location", loc.String())
	}
	if asn := rd.ASN; asn != nil {
		fmt.Fprintf(w, "%-20s %s (AS%v)\n", "ISP", asn.Name, asn.Number)
	}

	if mac := rd.MAC; mac != nil {
		fmt.Fprintf(w, "%-20s %s\n", "MAC Address", mac.Address)
		if vendor := mac.Vendor; vendor != "" {
			fmt.Fprintf(w, "%-20s %s\n", "NIC Vendor", vendor)
		}
	}

	if tls := rd.HTTP.TLS; tls != nil {
		fmt.Fprintf(w, "%-20s HTTPS (TLS %s / %s)\n", "Scheme", tls.Version, tls.Cipher)
	} else {
		fmt.Fprintf(w, "%-20s %s\n", "Scheme", "HTTP")
	}
	fmt.Fprintf(w, "%-20s %s\n", "Protocol", rd.HTTP.Protocol)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "HTTP Headers\n")
	fmt.Fprintf(w, "------------\n")

	colWidth := 20
	for _, h := range rd.HTTP.Headers {
		if newLen := len(h.Name); newLen > colWidth {
			colWidth = newLen
		}
	}

	for _, h := range rd.HTTP.Headers {
		fmt.Fprintf(w, "%-*s %s\n", colWidth, h.Name, h.Value)
	}
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "Server\n")
	fmt.Fprintf(w, "------\n")
	fmt.Fprintf(w, "%-20s %s\n", "Time", rd.Server.Time)
	fmt.Fprintf(w, "%-20s %s\n", "App Version", version.ToString())
	fmt.Fprintf(w, "%-20s %s\n", "Go Version", rd.Server.Versions.Go)
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind City", rd.Server.Versions.MaxMindCity)
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind ASN", rd.Server.Versions.MaxMindASN)
	fmt.Fprintln(w)
}
