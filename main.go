package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/oschwald/geoip2-golang/v2"
)

type requestData struct {
	IP          string            `json:"ip"`
	IPStack     string            `json:"ipStack"`
	Headers     map[string]string `json:"headers"`
	ISP         string            `json:"isp"`
	ASN         uint              `json:"asn"`
	City        string            `json:"city"`
	CountryCode string            `json:"country_code"`
	CountryName string            `json:"country_name"`
	ServerTime  string            `json:"server_time"`
}

type dualStackConfig struct {
	IPv4Host string `json:"IPv4"`
	IPv6Host string `json:"IPv6"`
	TryStack string `json:"tryStack"`
}

type jsonpResponse struct {
	Meta any `json:"meta"`
	Data any `json:"data"`
}

type pageData struct {
	Title     string
	Request   *requestData
	DualStack *dualStackConfig
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
		slog.Warn("MaxMind City DB Load", "err", err)
	}

	mmASN, err = geoip2.Open("data/maxmind/GeoLite2-ASN.mmdb")
	if err != nil {
		slog.Warn("MaxMind ASN DB Load", "err", err)
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Heartbeat("/healthz"))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: false,
	}))

	r.Get("/json", jsonHandler)
	r.Get("/jsonp", jsonpHandler)
	r.Get("/text", textHandler)
	r.Get("/ip", ipHandler)
	r.Get("/images/asn/{asn}.png", getAsnImage)
	r.Get("/", contentNegotiate(map[string]http.HandlerFunc{
		"application/json": jsonHandler,
		"text/html":        htmlHandler,
		"text/plain":       textHandler,
	}, "text/html"))
	fileServer := http.FileServer(http.Dir("./static"))
	r.Handle("/*", fileServer)

	binding := fmt.Sprintf(":%v", listenPort())
	slog.Info("HTTP server listening", "binding", binding)
	log.Fatal(http.ListenAndServe(binding, r))
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

func buildDualStack(r *http.Request) *dualStackConfig {
	addr := remoteAddr(r)

	var tryStack string

	if addr.Is4() {
		tryStack = "IPv6"
	} else {
		tryStack = "IPv4"
	}

	dualStackConfig := &dualStackConfig{
		IPv4Host: os.Getenv("IPV4_HOST"),
		IPv6Host: os.Getenv("IPV6_HOST"),
		TryStack: tryStack,
	}

	return dualStackConfig
}

func htmlHandler(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)

	page := &pageData{
		Title:     rd.IP,
		Request:   rd,
		DualStack: buildDualStack(r),
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

func jsonpHandler(w http.ResponseWriter, r *http.Request) {
	data := buildRequestdata(r)

	callback := r.URL.Query().Get("callback")
	if callback == "" {
		http.Error(w, "missing jsonp callback name", http.StatusBadRequest)
		return
	}

	resp := &jsonpResponse{
		Data: data,
	}

	w.Header().Add("content-type", "text/javascript")
	w.Write([]byte(callback + "("))

	enc := json.NewEncoder(w)
	enc.Encode(resp)

	w.Write([]byte(")"))
}

func textHandler(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)

	w.Header().Add("content-type", "text/plain")

	fmt.Fprintf(w, "%s\n", strings.Repeat("-", strings.Count(rd.IP, "")-1))
	fmt.Fprintf(w, "%s\n", rd.IP)
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("-", strings.Count(rd.IP, "")-1))
	fmt.Fprintf(w, "%-20s %s, %s\n", "Location", rd.City, rd.CountryName)

	if rd.CountryCode != "" {
		fmt.Fprintf(w, "%-20s %s, %s\n", "Location", rd.City, rd.CountryName)
	}
	if rd.ISP != "" {
		fmt.Fprintf(w, "%-20s %s (AS%v)\n", "ISP", rd.ISP, rd.ASN)
	}
	fmt.Fprintf(w, "%-20s %s\n", "Server Time", rd.ServerTime)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "HTTP Headers\n")
	for k, v := range rd.Headers {
		fmt.Fprintf(w, "  %-40s %s\n", k, v)
	}
}

func ipHandler(w http.ResponseWriter, r *http.Request) {
	addr := remoteAddr(r)

	w.Header().Add("content-type", "text/plain")
	fmt.Fprintln(w, addr.String())
}

func getAsnImage(w http.ResponseWriter, r *http.Request) {
	asn := chi.URLParam(r, "asn")

	if isMatch, err := regexp.MatchString("^[0-9]+$", asn); err != nil || !isMatch {
		slog.Info("Bad ASN image request", "asn", asn)
		http.NotFound(w, r)
		return
	}

	dir := "./cache/images/asn"
	imagePath := filepath.Join(dir, asn+".png")
	if _, err := os.Stat(imagePath); err == nil {
		slog.Debug("Found matching ASN image", "asn", asn)
		http.ServeFile(w, r, imagePath)
		return
	}

	slog.Info("No cached ASN logo found", "path", imagePath)

	sourceUrl := fmt.Sprintf("https://static.ui.com/asn/%s_101x101.png", asn)
	resp, err := http.Get(sourceUrl)
	if err != nil {
		slog.Info("Failed to download ASN logo", "url", sourceUrl, "err", err)
		http.NotFound(w, r)
		return
	}

	slog.Info("Trying to fetch ASN logo", "url", sourceUrl)

	if resp.StatusCode != 200 {
		slog.Info("No ASN logo", "url", sourceUrl, "status", resp.StatusCode)
		http.NotFound(w, r)
		return
	}

	out, err := os.Create(imagePath)
	if err != nil {
		slog.Warn("Failed to save ASN logo", "path", imagePath, "err", err)
		http.NotFound(w, r)
		return
	}

	slog.Info("Saving ASN logo", "asn", asn, "path", imagePath)
	io.Copy(out, resp.Body)
	http.ServeFile(w, r, imagePath)
}

func buildRequestdata(r *http.Request) *requestData {
	addr := remoteAddr(r)
	ipStack := ""
	if addr.Is4() {
		ipStack = "IPv4"
	} else {
		ipStack = "IPv6"
	}

	rd := &requestData{
		IP:         addr.String(),
		IPStack:    ipStack,
		ServerTime: time.Now().UTC().Format(time.DateTime) + " UTC",
	}

	// don't show headers that were set by a reverse proxy
	ignoreHeaders := []string{
		"x-cluster-client-ip",
		"x-real-ip",
		"x-forwarded-for",
		"x-forwarded-proto",
		"x-forwarded-host",
	}
	headers := make(map[string]string)
	for k, v := range r.Header {
		found := slices.Contains(ignoreHeaders, strings.ToLower(k))
		if !found {
			headers[k] = strings.Join(v, "; ")
		}
	}
	rd.Headers = headers

	// Lookup the location based on the IP address
	location, err := locateIP(addr)
	if err == nil {
		rd.City = location.City.Names.English
		rd.CountryCode = location.Country.ISOCode
		rd.CountryName = location.Country.Names.English
		slog.Info("City lookup OK", "ip", addr, "city", rd.City, "country", rd.CountryCode)
	} else {
		slog.Error("City lookup", "ip", addr, "err", err)
	}

	// Lookup the ASN which is typically the ISP. Close enough.
	asn, err := lookupASN(addr)
	if err == nil {
		rd.ISP = asn.AutonomousSystemOrganization
		rd.ASN = asn.AutonomousSystemNumber
		slog.Info("ASN lookup OK", "ip", addr, "org", rd.ISP, "asn", rd.ASN)
	} else {
		slog.Error("ASN lookup", "ip", addr, "err", err)
	}

	return rd
}

func remoteAddr(r *http.Request) netip.Addr {
	var host string
	if val := r.Header.Get("x-real-ip"); val != "" {
		host = val
	} else if val := r.Header.Get("x-forwarded-for"); val != "" {
		host = strings.Split(val, ",")[0]
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
