package app

import (
	"net/http"

	"github.com/mroach/whoami/internal/hitcounter"
)

func (app *App) ListRecentHandler(w http.ResponseWriter, _ *http.Request) {
	if app.HitCounter == nil {
		http.Error(w, "Not available", http.StatusServiceUnavailable)
		return
	}

	hits, err := app.HitCounter.ListRecent(100)
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
