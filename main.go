package main

import (
	"bytes"
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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/mroach/whoami/internal/version"
	"github.com/oschwald/geoip2-golang/v2"
)

type serverData struct {
	Time        string `json:"time"`
	GoVersion   string `json:"go_version"`
	AppVersion  string `json:"app_version"`
	MaxMindCity string `json:"maxmind_city"`
	MaxMindASN  string `json:"maxmind_asn"`
}

type requestData struct {
	IP          string            `json:"ip"`
	IPStack     string            `json:"ipStack"`
	Headers     map[string]string `json:"headers"`
	ISP         string            `json:"isp"`
	ASN         uint              `json:"asn"`
	City        string            `json:"city"`
	CountryCode string            `json:"country_code"`
	CountryName string            `json:"country_name"`
	Server      *serverData       `json:"server"`
}

type dualStackConfig struct {
	IPv4Host string `json:"IPv4"`
	IPv6Host string `json:"IPv6"`
	TryStack string `json:"tryStack"`
}

type pageData struct {
	Title     string
	Request   *requestData
	DualStack *dualStackConfig
}

var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
}

var templates = template.Must(template.New("pages").Funcs(funcMap).ParseFiles("templates/index.html"))
var mmCity *geoip2.Reader
var mmASN *geoip2.Reader

func main() {
	var err error

	// reconfigure default logging to allow customising the level
	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: getLogLevel()})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	slog.Info("Starting", "ver", version.ToString())

	mmCity, err = geoip2.Open("data/maxmind/GeoLite2-City.mmdb")
	if err != nil {
		slog.Warn("MaxMind City DB Load", "err", err)
	}
	if mmCity != nil {
		slog.Info("Loaded MaxMind City database", "built", mmCity.Metadata().BuildTime().Format(time.DateTime))
	}

	mmASN, err = geoip2.Open("data/maxmind/GeoLite2-ASN.mmdb")
	if err != nil {
		slog.Warn("MaxMind ASN DB Load", "err", err)
	}
	if mmASN != nil {
		slog.Info("Loaded MaxMind ASN database", "built", mmASN.Metadata().BuildTime().Format(time.DateTime))
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Heartbeat("/healthz"))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: false,
	}))

	r.Get("/json", jsonHandler)
	r.Get("/ip_info", ipInfoHandler)
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

func getLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
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
		for v := range strings.SplitSeq(r.Header.Get("accept"), ",") {
			accept := strings.TrimSpace(strings.Split(v, ";")[0])
			if handler := handlers[accept]; handler != nil {
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
	renderHtml(w, r, "index.html")
}

func renderHtml(w http.ResponseWriter, r *http.Request, templateName string) {
	rd := buildRequestdata(r)

	page := &pageData{
		Title:     rd.IP,
		Request:   rd,
		DualStack: buildDualStack(r),
	}

	w.Header().Add("content-type", "text/html")

	err := templates.Funcs(funcMap).ExecuteTemplate(w, templateName, page)
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

// used for XHR to get dual-stack IP info.
// older browsers will ask for jsonp by specifying the `callback` URL param.
// in other cases, render plain JS.
func ipInfoHandler(w http.ResponseWriter, r *http.Request) {
	data := buildRequestdata(r)

	var buf bytes.Buffer
	templates.Funcs(funcMap).ExecuteTemplate(&buf, "ipInfo", data)

	payload, _ := json.Marshal(struct {
		Data any    `json:"data"`
		HTML string `json:"html"`
	}{HTML: buf.String(), Data: data})

	w.Header().Add("content-type", "text/javascript")

	if callback := r.URL.Query().Get("callback"); callback != "" {
		fmt.Fprintf(w, "%s(%s);", template.JSEscapeString(callback), payload)
	} else {
		w.Write(payload)
	}
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
	fmt.Fprintf(w, "%-20s %s\n", "Server Time", rd.Server.Time)
	fmt.Fprintf(w, "%-20s %s\n", "Go Version", rd.Server.GoVersion)
	fmt.Fprintf(w, "%-20s %s\n", "App Version", version.ToString())
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind City", rd.Server.MaxMindCity)
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind ASN", rd.Server.MaxMindASN)
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
		IP:      addr.String(),
		IPStack: ipStack,
		Server: &serverData{
			Time:        time.Now().UTC().Format(time.DateTime) + " UTC",
			GoVersion:   fmt.Sprintf("%s %s-%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
			AppVersion:  version.ToString(),
			MaxMindCity: "N/A",
			MaxMindASN:  "N/A",
		},
	}

	if mmCity != nil {
		rd.Server.MaxMindCity = mmCity.Metadata().BuildTime().Format(time.DateTime)
	}

	if mmASN != nil {
		rd.Server.MaxMindASN = mmASN.Metadata().BuildTime().Format(time.DateTime)
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
	raddr := r.RemoteAddr

	slog.Debug("Remote address detected as", "raddr", raddr)

	// there may be a client port e.g. `10.8.0.1:23422`
	if isMatch, _ := regexp.MatchString("^(\\d+\\.){3}\\d+:[0-9]+$", raddr); isMatch {
		host, _, _ := net.SplitHostPort(raddr)
		slog.Debug("Found a port in the IPv4 address", "old", raddr, "new", host)
		raddr = host
	}

	// or, could be an IPv6 address with a port
	if isMatch, _ := regexp.MatchString("^\\[[^\\]]+\\]:[0-9]+$", raddr); isMatch {
		host, _, _ := net.SplitHostPort(raddr)
		slog.Debug("Found a port in the IPv6 address", "old", raddr, "new", host)
		raddr = host
	}

	addr, err := netip.ParseAddr(raddr)
	if err != nil {
		slog.Error("Failed to parse the host", "raddr", raddr, "err", err)
	}
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
