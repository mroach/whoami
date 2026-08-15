package app

import (
	"fmt"
	"image/gif"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func serveAsnImage(w http.ResponseWriter, r *http.Request, imagePath string, wantedFormat string) (ok bool) {
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

	ok = true
	defer func() {
		if !ok {
			http.NotFound(w, r)
		}
	}()

	if wantedFormat != "gif" {
		ok = false
		return
	}

	slog.Info("Converting to GIF", "path", imagePath)

	src, err := os.Open(imagePath)
	if err != nil {
		slog.Warn("Failed open source image", "path", imagePath, "err", err)
		ok = false
		return
	}
	defer src.Close()

	img, err := png.Decode(src)
	if err != nil {
		slog.Warn("Failed decode source image", "path", imagePath, "err", err)
		ok = false
		return
	}

	out, err := os.Create(wantedPath)
	if err != nil {
		slog.Warn("Failed to create file", "path", wantedPath, "err", err)
		ok = false
		return
	}
	defer out.Close()

	if err := gif.Encode(out, img, nil); err != nil {
		return
	}

	http.ServeFile(w, r, wantedPath)
	return
}

func (app *App) ASNImageHandler(w http.ResponseWriter, r *http.Request) {
	wantedFormat := chi.URLParam(r, "fmt")

	asn, err := strconv.Atoi(chi.URLParam(r, "asn"))
	if err != nil {
		slog.Info("Bad ASN image request", "asn", asn, "fmt", wantedFormat)
		http.NotFound(w, r)
		return
	}

	dir := app.Config.ASNImageDir
	imagePath := filepath.Join(dir, strconv.Itoa(asn)+".png")
	if _, err := os.Stat(imagePath); err == nil {
		slog.Debug("Found matching ASN image", "asn", asn)
		serveAsnImage(w, r, imagePath, wantedFormat)
		return
	}

	slog.Info("No cached ASN logo found", "path", imagePath)

	sourceUrl := fmt.Sprintf("https://static.ui.com/asn/%d_101x101.png", asn)
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
