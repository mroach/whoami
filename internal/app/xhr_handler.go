package app

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"text/template"
)

// used for XHR to get dual-stack IP info.
// older browsers will ask for jsonp by specifying the `callback` URL param.
// in other cases, render plain JS.
func (app *App) XHRHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)

	var buf bytes.Buffer
	if err := templates.Funcs(funcMap).ExecuteTemplate(&buf, "ipInfo", rd); err != nil {
		slog.Error("Template rendering failed", "err", err)
		http.NotFound(w, r)
		return
	}

	payload := struct {
		XMLName xml.Name `xml:"Data" json:"-"`
		Request any      `json:"request"`
		HTML    string   `json:"html"`
	}{HTML: buf.String(), Request: rd}

	if callback := r.URL.Query().Get("callback"); callback != "" {
		slog.Debug("Responding with JSONP", "callback", callback)
		json, _ := json.Marshal(payload)
		w.Header().Add("content-type", "application/javascript")
		fmt.Fprintf(w, "%s(%s);", template.JSEscapeString(callback), json)
	} else {
		slog.Debug("Responding with XML")
		bytes, _ := xml.MarshalIndent(payload, "", "  ")
		w.Header().Add("content-type", "application/xml")
		w.Write(bytes)
	}
}
