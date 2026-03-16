package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang/v2"
)

type requestData struct {
	IP          string            `json:"ip"`
	Headers     map[string]string `json:"headers"`
	ISP         string            `json:"isp"`
	ASN         uint              `json:"asn"`
	City        string            `json:"city"`
	Prefix      string            `json:"prefix"`
	CountryCode string            `json:"country_code"`
	CountryName string            `json:"country_name"`
	ServerTime  string            `json:"server_time"`
	Hostname    string            `json:"hostname"`
}

type pageData struct {
	Title   string
	Request *requestData
}

var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
}

var templates = template.Must(template.New("pages").Funcs(funcMap).ParseFiles("pages/index.html"))
var mmCity *geoip2.Reader
var mmASN *geoip2.Reader

func main() {
	var err error

	mmCity, err = geoip2.Open("data/maxmind/GeoLite2-City.mmdb")
	if err != nil {
		log.Printf("WARN: %v", err)
	}

	mmASN, err = geoip2.Open("data/maxmind/GeoLite2-ASN.mmdb")
	if err != nil {
		log.Printf("WARN: %v", err)
	}

	mux := http.NewServeMux()
	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("GET /", fileServer)
	mux.HandleFunc("GET /json", jsonHandler)
	mux.HandleFunc("GET /text", textHandler)
	mux.HandleFunc("GET /ip", ipHandler)
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /{$}", contentNegotiate(map[string]http.HandlerFunc{
		"application/json": jsonHandler,
		"text/html":        htmlHandler,
		"text/plain":       textHandler,
	}, "text/html"))

	binding := fmt.Sprintf(":%v", listenPort())

	log.Printf("Listening on %s", binding)

	log.Fatal(http.ListenAndServe(binding, logRequest(mux)))
}

type StatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *StatusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		rec := &StatusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		elapsedTime := time.Since(startTime)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "time", elapsedTime)
	})
}

func listenPort() int {
	strport := os.Getenv("PORT")
	i, err := strconv.Atoi(strport)
	if err != nil && i > 0 {
		return i
	}
	return 8080
}

func contentNegotiate(handlers map[string]http.HandlerFunc, fallback string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("accept")

		for contentType, handler := range handlers {
			if strings.Contains(accept, contentType) {
				handler(w, r)
				return
			}
		}

		handler := handlers[fallback]
		handler(w, r)
	}
}

func htmlHandler(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)
	page := &pageData{
		Title:   rd.IP,
		Request: rd,
	}

	w.Header().Add("content-type", "text/html")

	err := templates.Funcs(funcMap).ExecuteTemplate(w, "index.html", page)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func jsonHandler(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)

	w.Header().Add("content-type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(rd)
}

func textHandler(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)

	w.Header().Add("content-type", "text/plain")

	if rd.Hostname != "" {
		fmt.Fprintf(w, "%-20s %s\n", "Hostname", rd.Hostname)
	}
	if rd.Prefix != "" {
		fmt.Fprintf(w, "%-20s %s\n", "Prefix", rd.Prefix)
	}
	if rd.CountryCode != "" {
		fmt.Fprintf(w, "%-20s %s, %s\n", "Location", rd.City, rd.CountryName)
	}
	if rd.ISP != "" {
		fmt.Fprintf(w, "%-20s %s (AS%v)\n", "ISP", rd.ISP, rd.ASN)
	}
	fmt.Fprintf(w, "%-20s %s\n", "Server Time", rd.ServerTime)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Headers\n")
	for k, v := range rd.Headers {
		fmt.Fprintf(w, "  %-40s %s\n", k, v)
	}
}

func ipHandler(w http.ResponseWriter, r *http.Request) {
	addr := remoteAddr(r)

	w.Header().Add("content-type", "text/plain")
	fmt.Fprintln(w, addr.String())
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("content-type", "text/plain")
	fmt.Fprintln(w, "OK")
}

func buildRequestdata(r *http.Request) *requestData {
	addr := remoteAddr(r)

	rd := &requestData{
		IP:         addr.String(),
		ServerTime: time.Now().UTC().Format(time.DateTime) + " UTC",
	}

	ignoreHeaders := []string{"x-cluster-client-ip", "x-real-ip"}
	headers := make(map[string]string)
	for k, v := range r.Header {
		found := slices.Contains(ignoreHeaders, strings.ToLower(k))
		if !found {
			headers[k] = strings.Join(v, "; ")
		}
	}
	rd.Headers = headers

	hosts, err := net.LookupAddr(addr.String())
	if err == nil && len(hosts) > 0 {
		rd.Hostname = hosts[0]
	}

	// Lookup the location based on the IP address
	location, err := locateIP(addr)
	if err == nil {
		rd.City = location.City.Names.English
		rd.CountryCode = location.Country.ISOCode
		rd.CountryName = location.Country.Names.English
		rd.Prefix = location.Traits.Network.String()
		log.Printf("Location for %v: %s, %s", addr, rd.City, rd.CountryCode)
	} else {
		log.Printf("Location lookup failed for %v: %v", addr, err)
	}

	// Lookup the ASN which is typically the ISP. Close enough.
	asn, err := lookupASN(addr)
	if err == nil {
		rd.ISP = asn.AutonomousSystemOrganization
		rd.ASN = asn.AutonomousSystemNumber
		log.Printf("ASN for %v: %s (AS%v)", addr, rd.ISP, rd.ASN)
	} else {
		log.Printf("ASN Lookup failed for %s: %v", addr, err)
	}

	return rd
}

func remoteAddr(r *http.Request) netip.Addr {
	var host string
	if val := r.Header.Get("x-real-ip"); val != "" {
		host = val
	} else if val := r.URL.Query().Get("__ip"); val != "" {
		host = val
	} else {
		host, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	addr, _ := netip.ParseAddr(host)
	return addr
}

func locateIP(addr netip.Addr) (*geoip2.City, error) {
	if mmCity == nil {
		return nil, fmt.Errorf("City database not loaded")
	}
	return mmCity.City(addr)
}

func lookupASN(addr netip.Addr) (*geoip2.ASN, error) {
	if mmASN == nil {
		return nil, fmt.Errorf("ASN database not loaded")
	}

	return mmASN.ASN(addr)
}
