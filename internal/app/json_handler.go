package app

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (app *App) JSONHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	app.logHit(rd)

	w.Header().Add("content-type", "application/json")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(rd)
	fmt.Println(w)
}
