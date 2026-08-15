package app

import (
	"log/slog"

	"github.com/mroach/whoami/internal/hitcounter"
	"github.com/mroach/whoami/internal/runtime_config"
	"github.com/oschwald/geoip2-golang/v2"
)

type App struct {
	Config     runtime_config.Config
	GeoIPCity  *geoip2.Reader
	GeoIPASN   *geoip2.Reader
	HitCounter *hitcounter.HitCounter
}

func New(config runtime_config.Config) *App {
	app := &App{Config: config}
	app.loadGeoIPASN()
	app.loadGeoIPCity()
	app.loadHitCounter()
	return app
}

func (app *App) Close() {
	if app.GeoIPASN != nil {
		app.GeoIPASN.Close()
	}

	if app.GeoIPCity != nil {
		app.GeoIPCity.Close()
	}

	if app.HitCounter != nil {
		app.HitCounter.Close()
	}
}

func (app *App) logHit(rd *RequestData) {
	if app.HitCounter == nil {
		return
	}

	event := hitcounter.HitEvent{
		IP:          rd.IP.Addr,
		UserAgent:   rd.HTTP.UserAgent,
		HttpVersion: rd.HTTP.Protocol,
		Scheme:      rd.HTTP.Scheme,
	}

	if loc := rd.Location; loc != nil {
		event.Country = loc.Country.ISOCode
	}

	if asn := rd.ASN; asn != nil {
		event.ASN = asn.Number
	}

	app.HitCounter.LogHit(event)
}

func (app *App) loadGeoIPASN() {
	if mm, err := geoip2.Open(app.Config.GeoIPASNPath); err == nil {
		app.GeoIPASN = mm
	} else {
		slog.Warn("Failed to load MaxMind ASN DB", "err", err)
	}
}

func (app *App) loadGeoIPCity() {
	if mm, err := geoip2.Open(app.Config.GeoIPCityPath); err == nil {
		app.GeoIPCity = mm
	} else {
		slog.Warn("Failed to load MaxMind City DB", "err", err)
	}
}

func (app *App) loadHitCounter() {
	hc, err := hitcounter.New(hitcounter.Config{
		DatabasePath: app.Config.HitcounterDBPath,
		BufferSize:   5,
	})

	if err == nil {
		app.HitCounter = hc
	} else {
		slog.Warn("HitCounter init failed", "err", err)
	}
}
