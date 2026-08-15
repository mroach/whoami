package app

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/mroach/whoami/internal/eui64"
	"github.com/mroach/whoami/internal/ouidb"
	"github.com/mroach/whoami/internal/version"
)

type Server struct {
	Time     string   `json:"time"`
	Versions Versions `json:"versions"`
}

type Versions struct {
	Go          string `json:"go"`
	App         string `json:"app"`
	MaxMindCity string `json:"maxmind_city"`
	MaxMindASN  string `json:"maxmind_asn"`
}

type TLS struct {
	Version string `json:"version"`
	Cipher  string `json:"cipher"`
}

type HTTPHeader struct {
	Name  string `json:"name" xml:"name,attr"`
	Value string `json:"value" xml:",chardata"`
}

type HTTP struct {
	Host      string       `json:"host"`
	Protocol  string       `json:"protocol"`
	Scheme    string       `json:"scheme"`
	TLS       *TLS         `json:"tls"`
	UserAgent string       `json:"-" xml:"-"`
	Headers   []HTTPHeader `json:"headers" xml:"Headers>Header"`
}

type IPAddress struct {
	Addr    netip.Addr `json:"-" xml:"-"`
	Address string     `json:"address" xml:",chardata"`
	Family  string     `json:"family" xml:"family,attr"`
}

type MACAddress struct {
	Address string `json:"address"`
	Vendor  string `json:"vendor"`
}

type Country struct {
	ISOCode string `json:"iso_code"`
	Name    string `json:"name"`
}

type Location struct {
	City    string  `json:"city"`
	Country Country `json:"country"`
}

type ASN struct {
	Number uint   `json:"number"`
	Name   string `json:"name"`
}

type RequestData struct {
	XMLName  xml.Name    `xml:"Request"`
	IP       IPAddress   `json:"ip"`
	MAC      *MACAddress `json:"mac"`
	Location *Location   `json:"location"`
	ASN      *ASN        `json:"asn"`
	HTTP     HTTP        `json:"http"`
	Server   Server      `json:"server"`
}

func (loc *Location) String() string {
	if loc.City == "" {
		return loc.Country.Name
	}

	return loc.City + ", " + loc.Country.Name
}

func (app *App) buildRequestData(r *http.Request) *RequestData {
	addr := remoteAddr(r)
	data := &RequestData{
		IP:       buildIPAddress(addr),
		MAC:      lookupMAC(addr),
		ASN:      app.lookupASN(addr),
		Location: app.locateIP(addr),
		HTTP:     buildHttp(r),
		Server:   app.buildServer(),
	}

	slog.Debug("Built request data", "data", data)

	return data
}

func buildIPAddress(addr netip.Addr) IPAddress {
	family := ""
	if addr.Is4() {
		family = "IPv4"
	} else {
		family = "IPv6"
	}
	return IPAddress{
		Addr:    addr,
		Address: addr.String(),
		Family:  family,
	}
}

func buildHttp(r *http.Request) HTTP {
	data := HTTP{
		Host:      r.Host,
		Scheme:    "http",
		Protocol:  r.Proto,
		Headers:   buildHttpHeaders(r),
		UserAgent: r.Header.Get("user-agent"),
	}

	// When using a reverse proxy, it's the one that knows the true HTTP scheme, protocol, and TLS data.
	if info, err := url.ParseQuery(r.Header.Get("x-internal-http")); err == nil && len(info) > 0 {
		if scheme := info.Get("scheme"); scheme != "" {
			data.Scheme = scheme
		}

		if proto := info.Get("proto"); proto != "" {
			data.Protocol = proto
		}
	}

	// Reverse proxies can set the `x-internal-tls` header to pass-along TLS information.
	// This should be a URL query e.g. `version=tls1.3;cipher=TLS_AES_128_GCM_SHA256`
	if query, err := url.ParseQuery(r.Header.Get("x-internal-tls")); err == nil {
		values := make(map[string]string)

		for k := range query {
			v := query.Get(k)

			// Caddy will leave the bare variable and fencing when the values isn't available,
			// so you can end up with a query like `cipher={http.request.tls.cipher_suite}`
			if v == "" || v[0] == '{' {
				continue
			}

			values[k] = v
		}

		if len(values) > 0 {
			data.Scheme = "https"

			data.TLS = &TLS{
				Cipher: values["cipher"],
			}

			if ver := regexp.MustCompile(`\d\.\d`).FindString(values["version"]); ver != "" {
				data.TLS.Version = ver
			}
		}
	}

	return data
}

func buildHttpHeaders(r *http.Request) []HTTPHeader {
	headers := make([]HTTPHeader, 0)

	// don't show headers that were set by a reverse proxy
	ignoreHeaders := []string{
		"x-cluster-client-ip",
		"x-real-ip",
		"x-forwarded-for",
		"x-forwarded-proto",
		"x-forwarded-host",
	}

	for k, v := range r.Header {
		// values set by our reverse proxy for us
		if strings.HasPrefix(strings.ToLower(k), "x-internal-") {
			continue
		}

		found := slices.Contains(ignoreHeaders, strings.ToLower(k))
		if !found {
			headers = append(headers, HTTPHeader{
				Name:  k,
				Value: strings.Join(v, "; "),
			})
		}
	}

	return headers
}

func (app *App) locateIP(addr netip.Addr) *Location {
	if app.GeoIPCity == nil {
		return nil
	}

	city, err := app.GeoIPCity.City(addr)
	if err != nil {
		slog.Warn("GeoIP City lookup returned an error", "addr", addr, "err", err)
	}

	if !city.HasData() {
		slog.Info("No GeoIP City data", "addr", addr)
		return nil
	}

	location := &Location{
		Country: Country{
			ISOCode: city.Country.ISOCode,
			Name:    city.Country.Names.English,
		},
	}

	// Avoid results like "Singapore, Singapore" or "Hong Kong, Hong Kong".
	// Typically also affects Macau, Andorra, Luxembourg, Monaco
	if city := city.City.Names.English; city != location.Country.Name {
		location.City = city
	}

	slog.Info("GeoIP location lookup successful", "ip", addr.String(), "location", location)

	return location
}

func (app *App) lookupASN(addr netip.Addr) *ASN {
	if app.GeoIPASN == nil {
		return nil
	}

	asn, err := app.GeoIPASN.ASN(addr)
	if err != nil {
		slog.Warn("GeoIP ASN lookup returned an error", "addr", addr, "err", err)
		return nil
	}

	if !asn.HasData() {
		slog.Info("No GeoIP ASN data", "addr", addr)
		return nil
	}

	slog.Info("GeoIP ASN lookup successful", "ip", addr.String(), "asn", asn.AutonomousSystemNumber)

	return &ASN{
		Number: asn.AutonomousSystemNumber,
		Name:   asn.AutonomousSystemOrganization,
	}
}

func lookupMAC(addr netip.Addr) *MACAddress {
	if !addr.Is6() {
		return nil
	}

	mac, ok := eui64.DetectEUI64(addr)
	if !ok {
		return nil
	}

	macData := MACAddress{Address: mac.String()}

	if vendor, ok := ouidb.Lookup(mac); ok {
		macData.Vendor = vendor
	}

	return &macData
}

func (app *App) buildServer() Server {
	versions := Versions{
		Go:          fmt.Sprintf("%s %s-%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
		App:         version.ToString(),
		MaxMindCity: "N/A",
		MaxMindASN:  "N/A",
	}

	if app.GeoIPASN != nil {
		versions.MaxMindASN = app.GeoIPASN.Metadata().BuildTime().Format(time.DateTime)
	}
	if app.GeoIPCity != nil {
		versions.MaxMindCity = app.GeoIPCity.Metadata().BuildTime().Format(time.DateTime)
	}

	return Server{
		Time:     time.Now().Format(time.DateTime) + " UTC",
		Versions: versions,
	}
}

func remoteAddr(r *http.Request) netip.Addr {
	// If the IP was set in x-forwarded-for, `GetClientIP` is where we get it
	raddr := middleware.GetClientIP(r.Context())

	// If no XFF was set, then we'll use the normal remote address
	if raddr == "" {
		raddr = r.RemoteAddr
	}

	slog.Debug("Remote address detected as", "raddr", raddr)

	if addr, err := netip.ParseAddr(raddr); err == nil {
		return addr
	}

	if addr, err := netip.ParseAddrPort(raddr); err == nil {
		return addr.Addr()
	}

	slog.Warn("No valid IP address from", "raddr", raddr)

	return netip.MustParseAddr("0.0.0.0")
}
