package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"image/gif"
	"image/png"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
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
	"github.com/mroach/whoami/internal/eui64"
	"github.com/mroach/whoami/internal/hitcounter"
	"github.com/mroach/whoami/internal/ouidb"
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

type tlsData struct {
	Version string `json:"version"`
	Cipher  string `json:"cipher"`
}

type httpInfo struct {
	Scheme  string            `json:"scheme"`
	Proto   string            `json:"proto"`
	Host    string            `json:"host"`
	Headers map[string]string `json:"headers"`
}

type requestData struct {
	IP          string      `json:"ip"`
	IPStack     string      `json:"ipStack"`
	MACAddress  string      `json:"macAddress"`
	MACVendor   string      `json:"macVendor"`
	ISP         string      `json:"isp"`
	ASN         uint        `json:"asn"`
	City        string      `json:"city"`
	CountryCode string      `json:"country_code"`
	CountryName string      `json:"country_name"`
	Server      *serverData `json:"server"`
	TLS         *tlsData    `json:"tls"`
	HTTP        *httpInfo   `json:"http"`
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
	CacheBust string
}

var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
	"ToUpper": strings.ToUpper,
}

var templates = template.Must(template.New("pages").Funcs(funcMap).ParseFiles(
	"templates/index.html3.html",
	"templates/index.html4.html",
	"templates/recent.html",
))
var mmCity *geoip2.Reader
var mmASN *geoip2.Reader
var hc *hitcounter.HitCounter

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

	var trustedProxies []string
	if str, ok := os.LookupEnv("TRUSTED_PROXIES"); ok {
		trustedProxies = regexp.MustCompile(`[;,\s]+`).Split(str, -1)
		slog.Info("Trusted proxies configured", "networks", trustedProxies)
	}

	hc, err = hitcounter.New(hitcounter.Config{
		DatabasePath: "run/db/hits.db",
		BufferSize:   5,
	})
	if err != nil {
		slog.Warn("HitCounter init failed", "err", err)
	}
	defer hc.Close()

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromXFF(trustedProxies...))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
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
	r.Get("/images/asn/{asn}.{fmt}", getAsnImage)
	r.Get("/images/visitor/{ts}.gif", getHitCounter)
	r.Get("/", contentNegotiate(map[string]http.HandlerFunc{
		"application/json": jsonHandler,
		"text/html":        htmlHandler,
		"text/plain":       textHandler,
	}, "text/html"))
	r.Get("/html{htmlVer}", htmlHandler)
	r.Get("/recent", listRecentHandler)
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
	rd := buildRequestdata(r)

	page := &pageData{
		Title:     rd.IP,
		Request:   rd,
		DualStack: buildDualStack(r),
		CacheBust: strconv.FormatInt(time.Now().UTC().Unix(), 32),
	}

	w.Header().Add("content-type", "text/html")

	templateName := "index.html4.html"

	// It's largely correct to say any browser that uses HTTP/1.0 can only handle HTML 3.2
	//   Netscape 2.0, IE 3.0, CyberDog 2.0, Lynx, Opera 2.0.
	// There are a couple exceptions, and these browsers only support HTML 2.0:
	//   Netscape 1.0, NCSA Mosaic 2.x
	// The first browsers with HTTP/1.1 support, also supported HTML 4:
	//   Netscape 4.0, IE 4.0, Opera 3.5
	if strings.EqualFold(rd.HTTP.Proto, "HTTP/1.0") {
		templateName = "index.html3.html"
	}

	// Callers can specify the version they want in the URL e.g. `/html3`
	if ver := chi.URLParam(r, "htmlVer"); ver != "" {
		versionedName := "index.html" + ver + ".html"
		if t := templates.Lookup(versionedName); t != nil {
			templateName = versionedName
		}
	}

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

	if rd.CountryCode != "" {
		fmt.Fprintf(w, "%-20s %s, %s\n", "Location", rd.City, rd.CountryName)
	}
	if rd.ISP != "" {
		fmt.Fprintf(w, "%-20s %s (AS%v)\n", "ISP", rd.ISP, rd.ASN)
	}

	if rd.MACAddress != "" {
		fmt.Fprintf(w, "%-20s %s\n", "MAC Address", rd.MACAddress)
	}

	if rd.MACVendor != "" {
		fmt.Fprintf(w, "%-20s %s\n", "NIC Vendor", rd.MACVendor)
	}

	if rd.TLS != nil {
		fmt.Fprintf(w, "%-20s HTTPS (TLS %s / %s)\n", "Scheme", rd.TLS.Version, rd.TLS.Cipher)
	} else {
		fmt.Fprintf(w, "%-20s %s\n", "Scheme", "HTTP")
	}
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "Server\n")
	fmt.Fprintf(w, "------\n")
	fmt.Fprintf(w, "%-20s %s\n", "Time", rd.Server.Time)
	fmt.Fprintf(w, "%-20s %s\n", "App Version", version.ToString())
	fmt.Fprintf(w, "%-20s %s\n", "Go Version", rd.Server.GoVersion)
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind City", rd.Server.MaxMindCity)
	fmt.Fprintf(w, "%-20s %s\n", "MaxMind ASN", rd.Server.MaxMindASN)
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "HTTP Headers\n")
	fmt.Fprintf(w, "------------\n")

	colWidth := 20
	for k := range rd.HTTP.Headers {
		if newLen := len(k); newLen > colWidth {
			colWidth = newLen
		}
	}

	for k, v := range rd.HTTP.Headers {
		fmt.Fprintf(w, "%-*s %s\n", colWidth, k, v)
	}
}

func ipHandler(w http.ResponseWriter, r *http.Request) {
	addr := remoteAddr(r)

	w.Header().Add("content-type", "text/plain")
	fmt.Fprintln(w, addr.String())
}

func listRecentHandler(w http.ResponseWriter, _ *http.Request) {
	if hc == nil {
		http.Error(w, "Not available", http.StatusServiceUnavailable)
		return
	}

	hits, err := hc.ListRecent(100)
	if err != nil {
		http.Error(w, "Failed", http.StatusInternalServerError)
		return
	}

	pageData := struct {
		Hits []hitcounter.LoggedHit
	}{Hits: hits}

	err = templates.Funcs(funcMap).ExecuteTemplate(w, "recent.html", pageData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func getHitCounter(w http.ResponseWriter, r *http.Request) {
	rd := buildRequestdata(r)

	if hc != nil {
		hc.LogHit(hitcounter.HitEvent{
			IP:          remoteAddr(r),
			UserAgent:   r.Header.Get("user-agent"),
			HttpVersion: rd.HTTP.Proto,
			Scheme:      rd.HTTP.Scheme,
			Country:     rd.CountryCode,
			ASN:         rd.ASN,
		})
	}

	http.ServeFile(w, r, "static/images/clear.gif")
}

func serveAsnImage(w http.ResponseWriter, r *http.Request, imagePath string, wantedFormat string) {
	slog.Info("Serving ASN image", "source", imagePath, "fmt", wantedFormat)

	dir, file := filepath.Split(imagePath)
	wantedExt := "." + wantedFormat

	if filepath.Ext(file) == wantedExt {
		http.ServeFile(w, r, imagePath)
		return
	}

	asn := strings.TrimSuffix(filepath.Base(imagePath), filepath.Ext(imagePath))
	wantedPath := filepath.Join(dir, asn+wantedExt)

	// if we already have a cached converted copy, use it
	if _, err := os.Stat(wantedPath); err == nil {
		http.ServeFile(w, r, wantedPath)
		return
	}

	if wantedFormat == "gif" {
		slog.Info("Converting to GIF", "path", imagePath)

		src, err := os.Open(imagePath)
		if err != nil {
			slog.Warn("Failed open source image", "path", imagePath, "err", err)
			goto not_found
		}
		defer src.Close()

		img, err := png.Decode(src)
		if err != nil {
			slog.Warn("Failed decode source image", "path", imagePath, "err", err)
			goto not_found
		}

		out, err := os.Create(wantedPath)
		if err != nil {
			slog.Warn("Failed to create file", "path", wantedPath, "err", err)
			goto not_found
		}
		defer out.Close()

		if err := gif.Encode(out, img, nil); err != nil {
			goto not_found
		}
		http.ServeFile(w, r, wantedPath)
		return
	}

not_found:
	http.NotFound(w, r)
}

func getAsnImage(w http.ResponseWriter, r *http.Request) {
	asn := chi.URLParam(r, "asn")
	wantedFormat := chi.URLParam(r, "fmt")

	if isMatch, err := regexp.MatchString("^[0-9]+$", asn); err != nil || !isMatch {
		slog.Info("Bad ASN image request", "asn", asn, "fmt", wantedFormat)
		http.NotFound(w, r)
		return
	}

	dir := "./cache/images/asn"
	imagePath := filepath.Join(dir, asn+".png")
	if _, err := os.Stat(imagePath); err == nil {
		slog.Debug("Found matching ASN image", "asn", asn)
		serveAsnImage(w, r, imagePath, wantedFormat)
		return
	}

	slog.Info("No cached ASN logo found", "path", imagePath)

	sourceUrl := fmt.Sprintf("https://static.ui.com/asn/%s_101x101.png", asn)
	slog.Info("Trying to fetch ASN logo", "url", sourceUrl)

	resp, err := http.Get(sourceUrl)
	if err != nil {
		slog.Info("Failed to download ASN logo", "url", sourceUrl, "err", err)
		http.NotFound(w, r)
		return
	}

	if resp.StatusCode != 200 {
		slog.Info("No ASN logo", "url", sourceUrl, "status", resp.StatusCode)
		http.NotFound(w, r)
		return
	}

	slog.Info("Saving ASN logo", "asn", asn, "path", imagePath)

	out, err := os.Create(imagePath)
	if err != nil {
		slog.Warn("Failed to create ASN logo file", "path", imagePath, "err", err)
		http.NotFound(w, r)
		return
	}
	defer out.Close()
	io.Copy(out, resp.Body)

	serveAsnImage(w, r, imagePath, wantedFormat)
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

	if addr.Is6() {
		if mac, ok := eui64.DetectEUI64(addr); ok {
			rd.MACAddress = mac.String()
			slog.Info("Detected an EUI-64 address", "mac", rd.MACAddress)

			if vendor, ok := ouidb.Lookup(mac); ok {
				rd.MACVendor = vendor
				slog.Info("Detected MAC vendor", "mac", rd.MACAddress, "vendor", rd.MACVendor)
			}
		}
	}

	rd.HTTP = &httpInfo{
		Host:   r.Host,
		Scheme: "http",
		Proto:  r.Proto,
	}

	// Behind a reverse proxy, this info should be overridden by a header.
	if info, err := url.ParseQuery(r.Header.Get("x-internal-http")); err == nil && len(info) > 0 {
		if scheme := info.Get("scheme"); scheme != "" {
			rd.HTTP.Scheme = scheme
		}

		if proto := info.Get("proto"); proto != "" {
			rd.HTTP.Proto = proto
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
			rd.HTTP.Scheme = "https"

			rd.TLS = &tlsData{
				Cipher: values["cipher"],
			}

			if ver := regexp.MustCompile(`\d\.\d`).FindString(values["version"]); ver != "" {
				rd.TLS.Version = ver
			}
		}
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
		if strings.HasPrefix(strings.ToLower(k), "x-internal-") {
			continue
		}

		found := slices.Contains(ignoreHeaders, strings.ToLower(k))
		if !found {
			headers[k] = strings.Join(v, "; ")
		}
	}
	rd.HTTP.Headers = headers

	// Lookup the location based on the IP address
	location, err := locateIP(addr)
	if err == nil {
		rd.CountryCode = location.Country.ISOCode
		rd.CountryName = location.Country.Names.English

		// Avoid results like "Singapore, Singapore" or "Hong Kong, Hong Kong".
		// Typically also affects Macau, Andorra, Luxembourg, Monaco
		if city := location.City.Names.English; city != rd.CountryName {
			rd.City = city
		}

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
