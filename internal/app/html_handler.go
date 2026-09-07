package app

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// This handler serves up different HTML versions with the objective of the browser
// having the best rendering experience possible *without* resorting to User-Agent sniffing.
//
// HTML 4.01 is the default version since any modern browser can understand it,
// and browsers that pre-date it can usually render it in a passable way.
//
// HTML 3.2 is selected when the browser uses HTTP/1.0, as there's a convenient
// and close-enough correlation with browsers that use HTTP/1.0 not supporting HTML 4.0.
//
// The modern HTML 5 version is selected when the client sends HTTP headers that
// roughly align with modern browsers like `Sec-Fetch-*`.
func (app *App) HTMLHandler(w http.ResponseWriter, r *http.Request) {
	rd := app.buildRequestData(r)
	pd := app.buildPageData(rd)

	var templateName string

	// Callers can specify the version they want in the URL e.g. `/html3`
	if ver := chi.URLParam(r, "htmlVer"); ver != "" {
		versionedName := "index.html" + ver + ".html"
		if t := templates.Lookup(versionedName); t != nil {
			templateName = versionedName
		}
	}

	// It's largely correct to say any browser that uses HTTP/1.0 can only handle HTML 3.2
	//   Netscape 2.0, IE 3.0, CyberDog 2.0, Lynx, Opera 2.0.
	// There are a couple exceptions, and these browsers only support HTML 2.0:
	//   Netscape 1.0, NCSA Mosaic 2.x
	// The first browsers with HTTP/1.1 support, also supported HTML 4:
	//   Netscape 4.0, IE 4.0, Opera 3.5
	if templateName == "" && strings.EqualFold(rd.HTTP.Protocol, "HTTP/1.0") {
		templateName = "index.html3.html"
	}

	// Modern browsers will send the `Sec-*` headers which were introduced
	// in the era of WebSocket and JS `fetch`.
	// This is the era of Chrome 76, Firefox 90, Safari 16 (2019-2021)
	// But, some won't send these unless the connection is HTTPS.
	// `Priority` is an even more modern header from 2024.
	if templateName == "" {
		for k, _ := range r.Header {
			if k == "Priority" || strings.HasPrefix(k, "Sec-") {
				templateName = "index.html5.html"
				break
			}
		}
	}

	// No matches yet? HTML 4 is the most compatible all-arounder.
	if templateName == "" {
		templateName = "index.html4.html"
	}

	w.Header().Add("content-type", "text/html")

	err := templates.Funcs(funcMap).ExecuteTemplate(w, templateName, pd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
