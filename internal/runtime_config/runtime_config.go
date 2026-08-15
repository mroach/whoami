package runtime_config

import (
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	// ASN_IMAGE_DIR
	ASNImageDir string

	// GEOIP_ASN_PATH
	// default: run/data/maxmind/GeoLite2-ASN.mmdb or /usr/share/GeoIP/GeoLite2-ASN.mmdb
	GeoIPASNPath string

	// GEOIP_CITY_PATH
	// default: run/data/maxmind/GeoLite2-City.mmdb or /usr/share/GeoIP/GeoLite2-City.mmdb
	GeoIPCityPath string

	// HITCOUNTER_DB_PATH
	// default: run/db/hits.db
	HitcounterDBPath string

	// IPV4_HOST
	IPv4Host string

	// IPV6_HOST
	IPv6Host string

	// PORT
	// default: 8080
	ListenPort int

	// LOG_LEVEL
	// default: info
	LogLevel slog.Level

	// TRUSTED_PROXIES
	// default: []
	TrustedProxies []string

	// URL_BASE
	URLBase string
}

func Current() Config {
	config := Config{
		LogLevel:         getLogLevel(),
		GeoIPASNPath:     getGeoIPPath("ASN"),
		GeoIPCityPath:    getGeoIPPath("City"),
		TrustedProxies:   getTrustedProxies(),
		HitcounterDBPath: getHitcounterDBPath(),
		ListenPort:       getListenPort(),
		IPv4Host:         os.Getenv("IPV4_HOST"),
		IPv6Host:         os.Getenv("IPV6_HOST"),
		URLBase:          getURLBase(),
		ASNImageDir:      getASNImageDir(),
	}

	return config
}

func getASNImageDir() string {
	return tryEnvOrDefaultPath("ASN_IMAGE_DIR", "run/cache/images/asn")
}

func getGeoIPPath(kind string) string {
	filename := "GeoLite2-" + kind + ".mmdb"

	return tryEnvOrDefaultPath(
		"GEOIP_"+strings.ToUpper(kind)+"_PATH",
		"run/data/maxmind/"+filename,
		"/usr/share/GeoIP/"+filename,
	)
}

func getHitcounterDBPath() string {
	return tryEnvOrDefaultPath("HITCOUNTER_DB_PATH", "run/db/hits.db")
}

func getListenPort() int {
	strport := os.Getenv("PORT")
	i, err := strconv.Atoi(strport)
	if err != nil && i > 0 {
		return i
	}
	return 8080
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

func getTrustedProxies() []string {
	trustedProxies := make([]string, 0)

	str, ok := os.LookupEnv("TRUSTED_PROXIES")
	if !ok {
		return trustedProxies
	}

	givenStrings := regexp.MustCompile(`[;,\s]+`).Split(str, -1)
	for _, s := range givenStrings {
		if ip := parseIPOrPrefix(s); ip != "" {
			trustedProxies = append(trustedProxies, ip)
		}
	}

	return trustedProxies
}

func getURLBase() string {
	rawUrl := os.Getenv("URL_BASE")
	if rawUrl == "" {
		return ""
	}

	parsed, err := url.Parse(rawUrl)
	if err != nil {
		slog.Warn("Not a valid URL. Ignoring.", "given", rawUrl, "err", err)
		return ""
	}

	return parsed.String()
}

func parseIPOrPrefix(s string) string {
	// prefix or CIDR e.g. 10.8.0.0/16
	if strings.Contains(s, "/") {
		prefix, err := netip.ParsePrefix(s)
		if err == nil {
			return prefix.String()
		}

		slog.Warn("Given network prefix is invalid", "given", s, "err", err)
		return ""
	}

	// bare IPs are *not* supported by the middleware, so we'll convert them to networks of 1
	ip, err := netip.ParseAddr(s)
	if err != nil {
		slog.Warn("Given IP is invalid", "given", s, "err", err)
		return ""
	}

	return netip.PrefixFrom(ip, ip.BitLen()).Masked().String()
}

// if a given path exists, use it, otherwise check the default path
func tryEnvOrDefaultPath(envname string, fallbacks ...string) string {
	if envpath := os.Getenv(envname); envpath != "" {
		if _, err := os.Stat(envpath); err == nil {
			return envpath
		} else {
			slog.Warn("Error accessing path configured from ENV ", "envname", envname, "path", envpath, "err", err)
		}
	}

	for _, fallback := range fallbacks {
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}

	}

	return ""
}
