package app

import (
	"html/template"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var funcMap = template.FuncMap{
	"ToLower": strings.ToLower,
	"ToUpper": strings.ToUpper,
}

var templates = template.Must(template.New("pages").Funcs(funcMap).ParseFiles(
	"templates/index.html3.html",
	"templates/index.html4.html",
	"templates/index.wml",
	"templates/recent.html",
))

type dualStackConfig struct {
	IPv4Host string `json:"IPv4"`
	IPv6Host string `json:"IPv6"`
	TryStack string `json:"tryStack"`
}

type pageData struct {
	Title     string
	URLBase   string
	Request   *RequestData
	DualStack dualStackConfig
	CacheBust string
}

func (app *App) buildPageData(rd *RequestData) pageData {
	urlBase := rd.HTTP.Scheme + "://" + rd.HTTP.Host

	if configuredUrl := app.Config.URLBase; configuredUrl != "" {
		base, _ := url.Parse(configuredUrl)
		base.Scheme = rd.HTTP.Scheme
		urlBase = base.String()
	}

	pd := pageData{
		Title:     rd.IP.Address,
		URLBase:   urlBase,
		Request:   rd,
		DualStack: app.buildDualStack(rd.IP.Addr),
		CacheBust: strconv.FormatInt(time.Now().UTC().Unix(), 32),
	}

	return pd
}

func (app *App) buildDualStack(addr netip.Addr) dualStackConfig {
	var tryStack string

	if addr.Is4() {
		tryStack = "IPv6"
	} else {
		tryStack = "IPv4"
	}

	dualStackConfig := dualStackConfig{
		IPv4Host: app.Config.IPv4Host,
		IPv6Host: app.Config.IPv6Host,
		TryStack: tryStack,
	}

	return dualStackConfig
}
