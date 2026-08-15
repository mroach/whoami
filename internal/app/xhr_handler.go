package app

import (
	"bytes"
	"encoding/json"
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

	payload, _ := json.Marshal(struct {
		Data any    `json:"data"`
		HTML string `json:"html"`
	}{HTML: buf.String(), Data: rd})

	if callback := r.URL.Query().Get("callback"); callback != "" {
		w.Header().Add("content-type", "application/javascript")
		fmt.Fprintf(w, "%s(%s);", template.JSEscapeString(callback), payload)
	} else {
		w.Header().Add("content-type", "application/json")
		w.Write(payload)
	}
}
