package app

import (
	"fmt"
	"net/http"
)

func (app *App) WAPHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	app.logHit(rd)

	pd := app.buildPageData(rd)

	w.Header().Add("content-type", "text/vnd.wap.wml")

	// can't put this in the .wml template since `html/template` will escape it into `&lt?xml`
	fmt.Fprintf(w, "<?xml version=\"1.0\"?>\n")
	templates.Funcs(funcMap).ExecuteTemplate(w, "index.wml", pd)
	fmt.Fprintln(w)
}
