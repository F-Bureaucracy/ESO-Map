package graph

import "strings"

// maxFlows bounds flow enumeration on pathological graphs.
const maxFlows = 10000

// computeFlows enumerates every maximal path through the graph, starting at
// nodes without incoming edges. Paths follow item-level edges, so a flow
// such as "OpenBao secret/app#password → ExternalSecret → Secret[password] →
// $DB_PASSWORD" does not wander into unrelated keys. An empty item on either
// side of an edge means "the whole node".
func computeFlows(g *Graph) []Flow {
	byID := make(map[string]*Node, len(g.Nodes))
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	out := map[string][]Edge{}
	hasIncoming := map[string]bool{}
	for _, e := range g.Edges {
		out[e.From] = append(out[e.From], e)
		hasIncoming[e.To] = true
	}

	var (
		flows  []Flow
		hops   []Hop
		types  []EdgeType
		onPath = map[Hop]bool{}
		walk   func(h Hop)
	)
	emit := func() {
		// Store -> remote key paths without any consumer carry no information.
		for _, t := range types {
			if t != EdgeContains {
				path := append([]Hop(nil), hops...)
				flows = append(flows, Flow{Summary: summarize(byID, path), Hops: path})
				return
			}
		}
	}
	walk = func(h Hop) {
		if len(flows) >= maxFlows {
			return
		}
		hops = append(hops, h)
		onPath[h] = true
		defer func() {
			hops = hops[:len(hops)-1]
			delete(onPath, h)
		}()

		advanced := false
		for _, e := range out[h.Node] {
			if h.Item != "" && e.FromItem != "" && e.FromItem != h.Item {
				continue
			}
			next := Hop{Node: e.To, Item: e.ToItem}
			if onPath[next] {
				continue
			}
			advanced = true
			// A node reached as a whole (e.g. a remote key via its store) is
			// narrowed to the item this edge actually reads, so summaries
			// show "path#property" rather than just "path".
			last := len(hops) - 1
			if h.Item == "" && e.FromItem != "" {
				hops[last].Item = e.FromItem
			}
			types = append(types, e.Type)
			walk(next)
			types = types[:len(types)-1]
			hops[last].Item = h.Item
		}
		if !advanced && len(hops) > 1 {
			emit()
		}
	}

	for _, n := range g.Nodes {
		if !hasIncoming[n.ID] {
			walk(Hop{Node: n.ID})
		}
	}
	return flows
}

func summarize(byID map[string]*Node, hops []Hop) string {
	parts := make([]string, 0, len(hops))
	for _, h := range hops {
		parts = append(parts, describeHop(byID[h.Node], h.Item))
	}
	return strings.Join(parts, " → ")
}

func describeHop(n *Node, item string) string {
	var s string
	switch n.Type {
	case NodeStore:
		s = n.Kind + " " + qualified(n.Namespace, n.Name)
		if b := n.Attributes["backend"]; b != "" {
			s = b + " (" + s + ")"
		}
	case NodeRemote:
		s = n.Label
		if item != "" {
			if it, ok := n.Item(item); ok {
				s += "#" + it.Label
			}
		}
		return s
	default:
		s = n.Kind + " " + qualified(n.Namespace, n.Name)
	}
	if item != "" {
		if it, ok := n.Item(item); ok {
			s += " [" + it.Label + "]"
		}
	}
	return s
}
