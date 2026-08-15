package app

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
)

func (app *App) XMLHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	app.logHit(rd)

	bytes, err := xml.MarshalIndent(rd, "", "  ")
	if err != nil {
		slog.Error("XML serialization failed", "err", err)
		http.Error(w, "Error", http.StatusInternalServerError)
		return
	}

	w.Header().Add("content-type", "application/xml")
	w.Write([]byte(xml.Header))
	w.Write(bytes)
	fmt.Fprintln(w)
}
