package app

import (
	"fmt"
	"net/http"
)

func (app *App) IPOnlyHandler(w http.ResponseWriter, r *http.Request) {
	addr := app.GetRemoteAddr(r)
	w.Header().Add("content-type", "text/plain")
	fmt.Fprintln(w, addr.String())
}
