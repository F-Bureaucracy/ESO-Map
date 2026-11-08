package graph

import (
	"strconv"

	"github.com/vincent/eso-map/internal/eso"
	"github.com/vincent/eso-map/internal/manifest"
)

// addPushSecret wires a PushSecret: Secret keys (or a generator) flow into
// its entries, which are written to remote keys in every selected store.
func (b *builder) addPushSecret(o manifest.Object, ns string, spec eso.PushSecretSpec) {
	n := b.node("push:"+o.Kind+"/"+qualified(ns, o.Name), func() *Node {
		return &Node{Type: NodePushSecret, Kind: o.Kind, Name: o.Name, Namespace: ns, Label: o.Name}
	})
	n.Source = b.source(o)
	setAttr(n, "updatePolicy", spec.UpdatePolicy)
	setAttr(n, "deletionPolicy", spec.DeletionPolicy)

	var (
		secret *Node
		gen    *Node
	)
	switch {
	case spec.Selector.Secret != nil && spec.Selector.Secret.Name != "":
		secret = b.consumedSecret(ns, spec.Selector.Secret.Name)
		setAttr(n, "source", "Secret "+spec.Selector.Secret.Name)
	case spec.Selector.GeneratorRef != nil:
		gen = b.resolveGenerator(spec.Selector.GeneratorRef, ns, o, n.ID)
		setAttr(n, "source", "generator "+spec.Selector.GeneratorRef.Kind+"/"+spec.Selector.GeneratorRef.Name)
	default:
		b.diag(SeverityWarning, n.ID, o, "%s %s has no secret or generator selector", o.Kind, qualified(o.Namespace, o.Name))
	}

	stores := b.pushStores(o, ns, n.ID, spec)

	if len(spec.Data) == 0 {
		b.diag(SeverityInfo, n.ID, o, "%s %s has no data entries and pushes nothing", o.Kind, qualified(o.Namespace, o.Name))
	}
	for i, d := range spec.Data {
		m := d.Match
		id := "data:" + strconv.Itoa(i)
		label := m.SecretKey
		if label == "" {
			label = "(whole secret)"
		}
		target := m.RemoteRef.RemoteKey
		if m.RemoteRef.Property != "" {
			target += "#" + m.RemoteRef.Property
		}
		n.AddItem(Item{ID: id, Label: label, Detail: "→ " + target})

		switch {
		case secret != nil:
			from := ""
			if m.SecretKey != "" {
				from = b.secretKey(secret, m.SecretKey, o, n.ID)
			}
			b.edge(Edge{From: secret.ID, FromItem: from, To: n.ID, ToItem: id, Type: EdgeSelect})
		case gen != nil:
			b.edge(Edge{From: gen.ID, To: n.ID, ToItem: id, Type: EdgeGenerate})
		}

		for _, store := range stores {
			remote := b.remote(store, m.RemoteRef.RemoteKey, "")
			to := remoteItem(remote, m.RemoteRef.Property, "")
			b.edge(Edge{From: n.ID, FromItem: id, To: remote.ID, ToItem: to, Type: EdgePush})
		}
	}
}

func (b *builder) pushStores(o manifest.Object, ns, fromID string, spec eso.PushSecretSpec) []*Node {
	var stores []*Node
	for _, ref := range spec.SecretStoreRefs {
		if ref.LabelSelector != nil {
			kind := ref.Kind
			if kind == "" {
				kind = eso.KindSecretStore
			}
			matched := false
			for _, id := range b.order {
				s := b.nodes[id]
				if s.Type != NodeStore || s.Kind != kind || s.Missing {
					continue
				}
				if kind == eso.KindSecretStore && s.Namespace != "" && ns != "" && s.Namespace != ns {
					continue
				}
				if ref.LabelSelector.Matches(b.labels[id]) {
					stores = append(stores, s)
					matched = true
				}
			}
			if !matched {
				b.diag(SeverityWarning, fromID, o, "%s %s: no %s matches its label selector", o.Kind, qualified(o.Namespace, o.Name), kind)
			}
			continue
		}
		storeRef := eso.StoreRef{Name: ref.Name, Kind: ref.Kind}
		if s := b.resolveStore(&storeRef, ns, o, fromID); s != nil {
			stores = append(stores, s)
		}
	}
	if len(spec.SecretStoreRefs) == 0 {
		b.diag(SeverityWarning, fromID, o, "%s %s has no secretStoreRefs", o.Kind, qualified(o.Namespace, o.Name))
	}
	return stores
}
