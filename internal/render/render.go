// Package render serialises a graph.Graph as JSON, YAML or a self-contained
// interactive HTML visualisation.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/vincent/eso-map/internal/graph"
)

// Format is an output format.
type Format string

// Supported formats.
const (
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
	FormatHTML Format = "html"
)

// Formats lists the supported formats.
func Formats() []Format { return []Format{FormatJSON, FormatYAML, FormatHTML} }

// ParseFormat validates a user-supplied format name (case-insensitive;
// "yml" is accepted as an alias for "yaml").
func ParseFormat(s string) (Format, error) {
	switch f := Format(strings.ToLower(s)); f {
	case FormatJSON, FormatYAML, FormatHTML:
		return f, nil
	case "yml":
		return FormatYAML, nil
	}
	return "", fmt.Errorf("unsupported output format %q (want one of %v)", s, Formats())
}

// Render writes g to w in format f.
func Render(w io.Writer, f Format, g *graph.Graph) error {
	switch f {
	case FormatJSON:
		return JSON(w, g)
	case FormatYAML:
		return YAML(w, g)
	case FormatHTML:
		return HTML(w, g)
	}
	return fmt.Errorf("unsupported output format %q", f)
}

// JSON writes g as indented JSON.
func JSON(w io.Writer, g *graph.Graph) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(g)
}

// YAML writes g as YAML.
func YAML(w io.Writer, g *graph.Graph) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(g); err != nil {
		return err
	}
	return enc.Close()
}
