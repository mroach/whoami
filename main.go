package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/mroach/whoami/internal/app"
	"github.com/mroach/whoami/internal/appconfig"
	"github.com/mroach/whoami/internal/version"
)

func main() {
	config := appconfig.Current()

	// reconfigure default logging to allow customising the level
	logHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: config.LogLevel})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	app := app.New(config)
	defer app.Close()

	slog.Info("Runtime Configuration", "config", config)
	slog.Info("Setting-up", "ver", version.ToString())

	// Setup a list of mime types we want to handle in order of *our* priority.
	// Some browsers do not send the `Accept:` header values in an order that makes sense.
	// For example, Safari 3.0 sends `application/xml` as the top priority.
	handlerMap := NewHandlerMap("text/html")
	handlerMap.Add("text/html", app.HTMLHandler)
	handlerMap.Add("application/xhtml+xml", app.HTMLHandler)
	handlerMap.Add("text/vnd.wap.wml", app.WAPHandler)
	handlerMap.Add("application/vnd.wap.wml", app.WAPHandler)
	handlerMap.Add("text/plain", app.TextHandler)
	handlerMap.Add("text/xml", app.XMLHandler)
	handlerMap.Add("application/xml", app.XMLHandler)
	handlerMap.Add("application/json", app.JSONHandler)
	handlerMap.Add("text/json", app.JSONHandler)
	handlerMap.Add("text/x-json", app.JSONHandler)

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
	r.Use(app.SetRemoteAddr)

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
	r.Get("/xml", app.XMLHandler)
	r.Get("/", contentNegotiate(handlerMap))
	r.Handle("/*", http.FileServer(http.Dir("./static")))

	binding := fmt.Sprintf(":%v", config.ListenPort)
	slog.Info("HTTP server listening", "binding", binding)
	log.Fatal(http.ListenAndServe(binding, r))
}

func contentNegotiate(hm *handlerMap) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		supportedMimes := make([]string, 0)
		for v := range strings.SplitSeq(r.Header.Get("accept"), ",") {
			accept := strings.TrimSpace(strings.Split(v, ";")[0])
			if accept != "" && accept != "*/*" {
				supportedMimes = append(supportedMimes, accept)
			}
		}

		handler := hm.FindBestHandler(supportedMimes)
		handler(w, r)
	}
}

type handlerMap struct {
	handlers map[string]http.HandlerFunc
	priority []string
	fallback string
}

func NewHandlerMap(fallbackMime string) *handlerMap {
	return &handlerMap{
		handlers: make(map[string]http.HandlerFunc, 0),
		priority: make([]string, 0),
		fallback: fallbackMime,
	}
}

func (hm *handlerMap) Add(mime string, handler http.HandlerFunc) {
	if _, exists := hm.handlers[mime]; exists {
		slog.Warn("Overwriting existing handler", "mime", mime)
	}
	hm.priority = append(hm.priority, mime)
	hm.handlers[mime] = handler
}

func (hm *handlerMap) DefaultHandler() http.HandlerFunc {
	return hm.handlers[hm.fallback]
}

func (hm *handlerMap) FindBestHandler(supportedMimes []string) http.HandlerFunc {
	for _, mime := range hm.priority {
		if slices.Contains(supportedMimes, mime) {
			return hm.handlers[mime]
		}
	}

	return hm.DefaultHandler()
}
