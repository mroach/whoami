package app

import "net/http"

func (app *App) HitCounterHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	app.logHit(rd)

	http.ServeFile(w, r, "static/images/clear.gif")
}
