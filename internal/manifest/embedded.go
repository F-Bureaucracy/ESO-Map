package manifest

import (
	"regexp"

	"go.yaml.in/yaml/v3"
)

// walkEmbedded calls fn for every Kubernetes object nested anywhere inside n
// (but not n itself), such as resource templates of composition tools. via
// names the enclosing object; when the nested object sits next to an "id"
// key the id is appended as "#id".
func walkEmbedded(n *yaml.Node, via string, fn func(child *yaml.Node, via string)) {
	visit := func(child, parent *yaml.Node) {
		if isObject(child) {
			childVia := via
			if id := scalar(parent, "id"); id != "" {
				childVia += "#" + id
			}
			fn(child, childVia)
		}
		walkEmbedded(child, via, fn)
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			visit(n.Content[i], n)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			visit(c, nil)
		}
	}
}

// isObject reports whether n looks like a Kubernetes object: a mapping with
// scalar apiVersion and kind and a metadata mapping. This excludes object
// references (no metadata) and OpenAPI schemas (non-scalar apiVersion).
func isObject(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	return scalar(n, "apiVersion") != "" && scalar(n, "kind") != "" && child(n, "metadata") != nil &&
		child(n, "metadata").Kind == yaml.MappingNode
}

// child returns the value node for key in mapping n, or nil.
func child(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// scalar returns the scalar value at key in mapping n, or "".
func scalar(n *yaml.Node, key string) string {
	if c := child(n, key); c != nil && c.Kind == yaml.ScalarNode {
		return c.Value
	}
	return ""
}

// idRefRe matches simple field references such as ${dbPassword.metadata.name}.
// Anything more complex (functions, operators) is left untouched.
var idRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)((?:\.[A-Za-z_][A-Za-z0-9_-]*)+)\}`)

// resolveIDReferences handles a pattern several composition tools share:
// a list of entries that each pair an "id" with an embedded object, where
// objects refer to each other as ${<id>.<field path>}. Such references are
// rewritten in place to the referenced field's value, so that e.g. a
// generatorRef "${dbPassword.metadata.name}" becomes the generator's own
// (possibly still templated) name and the two objects can be linked.
// References that cannot be resolved this way are left as-is.
func resolveIDReferences(root *yaml.Node) {
	byID := map[string]*yaml.Node{}
	collectIDObjects(root, byID)
	if len(byID) == 0 {
		return
	}

	lookup := func(id, path string) (string, bool) {
		n := byID[id]
		for _, key := range splitPath(path) {
			n = child(n, key)
		}
		if n == nil || n.Kind != yaml.ScalarNode {
			return "", false
		}
		return n.Value, true
	}

	// References can chain (a -> b -> c), so resolve until stable.
	for range 8 {
		changed := false
		forEachScalar(root, func(s *yaml.Node) {
			out := idRefRe.ReplaceAllStringFunc(s.Value, func(m string) string {
				sub := idRefRe.FindStringSubmatch(m)
				if v, ok := lookup(sub[1], sub[2]); ok && v != m {
					return v
				}
				return m
			})
			if out != s.Value {
				s.Value = out
				changed = true
			}
		})
		if !changed {
			return
		}
	}
}

// collectIDObjects finds mappings with a scalar "id" and an embedded object
// (or object reference with apiVersion/kind/metadata) as one of their values.
func collectIDObjects(n *yaml.Node, byID map[string]*yaml.Node) {
	switch n.Kind {
	case yaml.MappingNode:
		if id := scalar(n, "id"); id != "" {
			for i := 1; i < len(n.Content); i += 2 {
				if isObject(n.Content[i]) {
					byID[id] = n.Content[i]
					break
				}
			}
		}
		for i := 1; i < len(n.Content); i += 2 {
			collectIDObjects(n.Content[i], byID)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			collectIDObjects(c, byID)
		}
	}
}

func splitPath(p string) []string {
	var parts []string
	start := 1 // skip leading "."
	for i := 1; i <= len(p); i++ {
		if i == len(p) || p[i] == '.' {
			parts = append(parts, p[start:i])
			start = i + 1
		}
	}
	return parts
}

func forEachScalar(n *yaml.Node, fn func(*yaml.Node)) {
	switch n.Kind {
	case yaml.ScalarNode:
		fn(n)
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			forEachScalar(n.Content[i], fn)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, c := range n.Content {
			forEachScalar(c, fn)
		}
	}
}
