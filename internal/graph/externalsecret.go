package graph

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/vincent/eso-map/internal/eso"
	"github.com/vincent/eso-map/internal/manifest"
)

func (b *builder) esNode(o manifest.Object) *Node {
	ns := o.Namespace
	if o.Kind == eso.KindClusterExternalSecret {
		ns = ""
	}
	n := b.node("es:"+o.Kind+"/"+qualified(ns, o.Name), func() *Node {
		return &Node{Type: NodeExternalSecret, Kind: o.Kind, Name: o.Name, Namespace: ns, Label: o.Name}
	})
	n.Source = b.source(o)
	return n
}

// addExternalSecret wires an ExternalSecret spec: its sources (stores or
// generators) into its entries, and its entries into the target Secret.
func (b *builder) addExternalSecret(o manifest.Object, n *Node, ns, defaultTarget string, spec eso.ExternalSecretSpec) {
	setAttr(n, "refreshInterval", spec.RefreshInterval)
	setAttr(n, "creationPolicy", spec.Target.CreationPolicy)

	// itemsByKey maps a data[].secretKey to the ExternalSecret item it creates.
	itemsByKey := map[string]string{}
	var keyOrder, dataFromItems []string

	for _, d := range spec.Data {
		id := "data:" + d.SecretKey
		n.AddItem(Item{ID: id, Label: d.SecretKey, Detail: describeRemoteRef(d.RemoteRef)})
		itemsByKey[d.SecretKey] = id
		keyOrder = append(keyOrder, d.SecretKey)

		if d.SourceRef != nil && d.SourceRef.GeneratorRef != nil {
			gen := b.resolveGenerator(d.SourceRef.GeneratorRef, ns, o, n.ID)
			b.edge(Edge{From: gen.ID, To: n.ID, ToItem: id, Type: EdgeGenerate})
			continue
		}
		ref := spec.SecretStoreRef
		if d.SourceRef != nil && d.SourceRef.StoreRef != nil {
			ref = d.SourceRef.StoreRef
		}
		store := b.resolveStore(ref, ns, o, n.ID)
		if store == nil {
			continue
		}
		remote := b.remote(store, d.RemoteRef.Key, "")
		from := remoteItem(remote, d.RemoteRef.Property, d.RemoteRef.Version)
		b.edge(Edge{From: remote.ID, FromItem: from, To: n.ID, ToItem: id, Type: EdgeFetch})
	}

	for i, df := range spec.DataFrom {
		id := "dataFrom:" + strconv.Itoa(i)
		item := Item{ID: id}
		if len(df.Rewrite) > 0 {
			item.Detail = "keys rewritten"
		}
		dataFromItems = append(dataFromItems, id)

		if df.SourceRef != nil && df.SourceRef.GeneratorRef != nil {
			g := df.SourceRef.GeneratorRef
			item.Label = "generator " + g.Kind + "/" + g.Name
			n.AddItem(item)
			gen := b.resolveGenerator(g, ns, o, n.ID)
			b.edge(Edge{From: gen.ID, To: n.ID, ToItem: id, Type: EdgeGenerate})
			continue
		}

		var remoteKey, remoteLabel, property, version string
		switch {
		case df.Extract != nil:
			item.Label = "extract " + df.Extract.Key
			remoteKey, property, version = df.Extract.Key, df.Extract.Property, df.Extract.Version
		case df.Find != nil:
			desc := describeFind(df.Find)
			item.Label = "find " + desc
			remoteKey, remoteLabel = "find:"+desc, "find "+desc
		default:
			item.Label = "dataFrom[" + strconv.Itoa(i) + "]"
			n.AddItem(item)
			continue
		}
		n.AddItem(item)

		ref := spec.SecretStoreRef
		if df.SourceRef != nil && df.SourceRef.StoreRef != nil {
			ref = df.SourceRef.StoreRef
		}
		store := b.resolveStore(ref, ns, o, n.ID)
		if store == nil {
			continue
		}
		remote := b.remote(store, remoteKey, remoteLabel)
		from := remoteItem(remote, property, version)
		b.edge(Edge{From: remote.ID, FromItem: from, To: n.ID, ToItem: id, Type: EdgeFetch})
	}

	// Target Secret.
	targetName := spec.Target.Name
	if targetName == "" {
		targetName = defaultTarget
	}
	secret := b.ensureSecret(ns, targetName)
	setAttr(secret, "managedBy", n.Kind+" "+qualified(n.Namespace, n.Name))
	setAttr(n, "target", targetName)

	tpl := spec.Target.Template
	templated := tpl != nil && (len(tpl.Data) > 0 || len(tpl.TemplateFrom) > 0)
	if tpl != nil {
		setAttr(secret, "type", tpl.Type)
		setAttr(n, "templateMergePolicy", tpl.MergePolicy)
	}

	syncWildcard := func(from string, detail string) {
		secret.AddItem(Item{ID: WildcardItem, Label: "* (runtime keys)", Detail: detail})
		b.edge(Edge{From: n.ID, FromItem: from, To: secret.ID, ToItem: WildcardItem, Type: EdgeSync})
	}

	if templated {
		for _, key := range sortedKeys(tpl.Data) {
			to := keyItem(key)
			secret.AddItem(Item{ID: to, Label: key, Detail: "templated"})
			refs := templateRefs(tpl.Data[key])
			unresolved := false
			for _, r := range refs {
				if from, ok := itemsByKey[r]; ok {
					b.edge(Edge{From: n.ID, FromItem: from, To: secret.ID, ToItem: to, Type: EdgeSync})
				} else {
					unresolved = true
				}
			}
			if unresolved || len(refs) == 0 {
				// The template may read keys produced by dataFrom.
				for _, from := range dataFromItems {
					b.edge(Edge{From: n.ID, FromItem: from, To: secret.ID, ToItem: to, Type: EdgeSync})
				}
			}
		}
		if len(tpl.TemplateFrom) > 0 {
			secret.AddItem(Item{ID: WildcardItem, Label: "* (runtime keys)", Detail: "templateFrom"})
		}
		if !strings.EqualFold(tpl.MergePolicy, "Merge") {
			return
		}
	}

	for _, key := range keyOrder {
		to := keyItem(key)
		secret.AddItem(Item{ID: to, Label: key})
		b.edge(Edge{From: n.ID, FromItem: itemsByKey[key], To: secret.ID, ToItem: to, Type: EdgeSync})
	}
	for _, from := range dataFromItems {
		syncWildcard(from, "from dataFrom")
	}
}

func setAttr(n *Node, k, v string) {
	if v != "" {
		n.Attributes[k] = v
	}
}

func describeRemoteRef(r eso.RemoteRef) string {
	s := r.Key
	if r.Property != "" {
		s += "#" + r.Property
	}
	if r.Version != "" {
		s += "@" + r.Version
	}
	return s
}

func describeFind(f *eso.Find) string {
	var parts []string
	if f.Path != "" {
		parts = append(parts, "path="+f.Path)
	}
	if f.Name != nil && f.Name.RegExp != "" {
		parts = append(parts, "name=~"+f.Name.RegExp)
	}
	for _, k := range sortedKeys(f.Tags) {
		parts = append(parts, fmt.Sprintf("tag:%s=%s", k, f.Tags[k]))
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, " ")
}

var (
	templateActionRe = regexp.MustCompile(`(?s)\{\{(.*?)\}\}`)
	templateFieldRe  = regexp.MustCompile(`(?:^|[^\w\.])\.([A-Za-z_][A-Za-z0-9_]*)`)
	templateIndexRe  = regexp.MustCompile(`index\s+\.\s+"([^"]+)"`)
)

// templateRefs extracts the secret keys a Go template reads, e.g.
// "{{ .user }}:{{ index . "pass-word" }}" yields [user pass-word].
func templateRefs(tpl string) []string {
	seen := map[string]bool{}
	var refs []string
	addRef := func(r string) {
		if !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	for _, action := range templateActionRe.FindAllStringSubmatch(tpl, -1) {
		body := action[1]
		for _, m := range templateIndexRe.FindAllStringSubmatch(body, -1) {
			addRef(m[1])
		}
		for _, m := range templateFieldRe.FindAllStringSubmatch(body, -1) {
			addRef(m[1])
		}
	}
	return refs
}
