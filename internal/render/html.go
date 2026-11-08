package render

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"

	"github.com/vincent/eso-map/internal/graph"
)

var (
	//go:embed assets/index.html.tmpl
	htmlTemplate string
	//go:embed assets/app.css
	appCSS string
	//go:embed assets/app.js
	appJS string
)

var page = template.Must(template.New("index").Parse(htmlTemplate))

// HTML writes a self-contained, offline-capable page that visualises g as
// columns of cards connected by arrows (provider path → ExternalSecret →
// Secret key → env var), with lineage highlighting, search and filters.
func HTML(w io.Writer, g *graph.Graph) error {
	// json.Marshal escapes <, > and &, so the payload is safe inside <script>.
	data, err := json.Marshal(g)
	if err != nil {
		return fmt.Errorf("encoding graph: %w", err)
	}
	return page.Execute(w, struct {
		Title string
		CSS   template.CSS
		JS    template.JS
		Data  template.JS
	}{
		Title: "ESO map · " + strings.Join(g.Roots, ", "),
		CSS:   template.CSS(appCSS),
		JS:    template.JS(appJS),
		Data:  template.JS(data),
	})
}
