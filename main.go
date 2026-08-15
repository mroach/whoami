package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/mroach/whoami/internal/app"
	"github.com/mroach/whoami/internal/runtime_config"
	"github.com/mroach/whoami/internal/version"
)

func main() {
	config := runtime_config.Current()

	// reconfigure default logging to allow customising the level
	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: config.LogLevel})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	app := app.New(config)
	defer app.Close()

	slog.Info("Runtime Configuration", "config", config)
	slog.Info("Setting-up", "ver", version.ToString())

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.ClientIPFromXFF(config.TrustedProxies...))
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(5 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET"},
		AllowCredentials: false,
	}))
	r.Use(middleware.Heartbeat("/healthz"))

	r.Get("/html", app.HTMLHandler)
	r.Get("/html{htmlVer:[3-5]}", app.HTMLHandler)
	r.Get("/images/asn/{asn:[0-9]+}.{fmt:(gif|png)}", app.ASNImageHandler)
	r.Get("/images/visitor/{ts}.gif", app.HitCounterHandler)
	r.Get("/ip", app.IPOnlyHandler)
	r.Get("/json", app.JSONHandler)
	r.Get("/recent", app.ListRecentHandler)
	r.Get("/text", app.TextHandler)
	r.Get("/wap", app.WAPHandler)
	r.Get("/xhr", app.XHRHandler)
	r.Get("/", contentNegotiate(map[string]http.HandlerFunc{
		"text/html":               app.HTMLHandler,
		"application/json":        app.JSONHandler,
		"text/plain":              app.TextHandler,
		"text/vnd.wap.wml":        app.WAPHandler,
		"application/vnd.wap.wml": app.WAPHandler,
	}, "text/html"))
	r.Handle("/*", http.FileServer(http.Dir("./static")))

	binding := fmt.Sprintf(":%v", config.ListenPort)
	slog.Info("HTTP server listening", "binding", binding)
	log.Fatal(http.ListenAndServe(binding, r))
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
