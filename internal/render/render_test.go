package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/vincent/eso-map/internal/graph"
)

func sampleGraph() *graph.Graph {
	return &graph.Graph{
		Roots: []string{"/repo</script><script>alert(1)</script>"},
		Nodes: []*graph.Node{
			{ID: "store:a", Type: graph.NodeStore, Kind: "ClusterSecretStore", Name: "a", Label: "a"},
			{ID: "secret:b", Type: graph.NodeSecret, Kind: "Secret", Name: "b", Label: "</script>"},
		},
		Edges: []graph.Edge{{From: "store:a", To: "secret:b", Type: graph.EdgeSync}},
	}
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{"json": FormatJSON, "YAML": FormatYAML, "yml": FormatYAML, "html": FormatHTML} {
		if got, err := ParseFormat(in); err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseFormat("table"); err == nil {
		t.Error("expected error")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatJSON, sampleGraph()); err != nil {
		t.Fatal(err)
	}
	var g graph.Graph
	if err := json.Unmarshal(buf.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 || g.Edges[0].Type != graph.EdgeSync {
		t.Errorf("round trip mismatch: %+v", g)
	}
}

func TestYAMLRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatYAML, sampleGraph()); err != nil {
		t.Fatal(err)
	}
	var g graph.Graph
	if err := yaml.Unmarshal(buf.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 2 || g.Nodes[0].ID != "store:a" {
		t.Errorf("round trip mismatch: %+v", g)
	}
}

func TestHTMLIsSelfContainedAndEscaped(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, FormatHTML, sampleGraph()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Count(out, "</script>") != 2 {
		t.Errorf("graph data must not be able to close the script tag; found %d </script>", strings.Count(out, "</script>"))
	}
	for _, want := range []string{"const ESO_GRAPH = {", "function trace(", "--c-store"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	for _, banned := range []string{"<script src=", "<link rel=\"stylesheet\""} {
		if strings.Contains(out, banned) {
			t.Errorf("output must not load external resources (%q)", banned)
		}
	}
}
