package app

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (app *App) HTMLHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	pd := app.buildPageData(rd)

	w.Header().Add("content-type", "text/html")

	templateName := "index.html4.html"

	// It's largely correct to say any browser that uses HTTP/1.0 can only handle HTML 3.2
	//   Netscape 2.0, IE 3.0, CyberDog 2.0, Lynx, Opera 2.0.
	// There are a couple exceptions, and these browsers only support HTML 2.0:
	//   Netscape 1.0, NCSA Mosaic 2.x
	// The first browsers with HTTP/1.1 support, also supported HTML 4:
	//   Netscape 4.0, IE 4.0, Opera 3.5
	if strings.EqualFold(rd.HTTP.Protocol, "HTTP/1.0") {
		templateName = "index.html3.html"
	}

	// Callers can specify the version they want in the URL e.g. `/html3`
	if ver := chi.URLParam(r, "htmlVer"); ver != "" {
		versionedName := "index.html" + ver + ".html"
		if t := templates.Lookup(versionedName); t != nil {
			templateName = versionedName
		}
	}

	err := templates.Funcs(funcMap).ExecuteTemplate(w, templateName, pd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
