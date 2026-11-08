package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/vincent/eso-map/internal/eso"
	"github.com/vincent/eso-map/internal/manifest"
)

// builder accumulates nodes, edges and diagnostics. It is not safe for
// concurrent use.
type builder struct {
	roots   []string
	nodes   map[string]*Node
	order   []string // insertion order, for deterministic lookups
	edges   []Edge
	edgeSet map[Edge]struct{}
	diags   []Diagnostic
	labels  map[string]map[string]string // store node ID -> metadata.labels
}

// Build turns decoded manifests into a Graph. roots are the scanned
// directories; source file paths are rendered relative to the parent of the
// root containing them, so "flux/infra/store.yaml" stays recognisable when
// several repositories are scanned together.
func Build(roots []string, objs []manifest.Object) *Graph {
	b := &builder{
		roots:   roots,
		nodes:   map[string]*Node{},
		edgeSet: map[Edge]struct{}{},
		labels:  map[string]map[string]string{},
	}

	// Resources are processed in dependency order so that references
	// resolve against already-known nodes.
	buckets := make([][]manifest.Object, phaseCount)
	for _, o := range objs {
		p := phaseOf(o)
		if p >= 0 {
			buckets[p] = append(buckets[p], o)
		}
	}
	for p, bucket := range buckets {
		for _, o := range bucket {
			if err := b.add(phase(p), o); err != nil {
				b.diag(SeverityWarning, "", o, "cannot decode %s %s: %v", o.Kind, o.Name, err)
			}
		}
	}

	b.reportSecrets()

	g := &Graph{Roots: roots, Edges: b.edges, Diagnostics: b.diags}
	for _, id := range b.order {
		g.Nodes = append(g.Nodes, b.nodes[id])
	}
	sortGraph(g)
	g.Flows = computeFlows(g)
	return g
}

type phase int

const (
	phaseStore phase = iota
	phaseGenerator
	phaseSecret
	phaseExternalSecret
	phasePushSecret
	phaseWorkload
	phaseCount
)

func phaseOf(o manifest.Object) phase {
	group := o.Group()
	switch {
	case group == eso.Group && (o.Kind == eso.KindSecretStore || o.Kind == eso.KindClusterSecretStore):
		return phaseStore
	case group == eso.GeneratorGroup:
		return phaseGenerator
	case group == "" && o.Kind == "Secret":
		return phaseSecret
	case group == eso.Group && (o.Kind == eso.KindExternalSecret || o.Kind == eso.KindClusterExternalSecret):
		return phaseExternalSecret
	case group == eso.Group && (o.Kind == eso.KindPushSecret || o.Kind == eso.KindClusterPushSecret):
		return phasePushSecret
	case group == eso.Group:
		return -1 // other ESO resources (e.g. status-only objects) are ignored
	default:
		return phaseWorkload
	}
}

func (b *builder) add(p phase, o manifest.Object) error {
	switch p {
	case phaseStore:
		return b.addStore(o)
	case phaseGenerator:
		return b.addGenerator(o)
	case phaseSecret:
		return b.addSecretManifest(o)
	case phaseExternalSecret:
		if o.Kind == eso.KindClusterExternalSecret {
			var ces eso.ClusterExternalSecret
			if err := o.Decode(&ces); err != nil {
				return err
			}
			target := ces.Spec.ExternalSecretName
			if target == "" {
				target = o.Name
			}
			n := b.esNode(o)
			if len(ces.Spec.Namespaces) > 0 {
				n.Attributes["namespaces"] = strings.Join(ces.Spec.Namespaces, ", ")
			}
			// The generated ExternalSecrets live in selected namespaces;
			// an empty namespace matches consumers in any namespace.
			b.addExternalSecret(o, n, "", target, ces.Spec.ExternalSecretSpec)
			return nil
		}
		var es eso.ExternalSecret
		if err := o.Decode(&es); err != nil {
			return err
		}
		b.addExternalSecret(o, b.esNode(o), o.Namespace, o.Name, es.Spec)
		return nil
	case phasePushSecret:
		if o.Kind == eso.KindClusterPushSecret {
			var cps eso.ClusterPushSecret
			if err := o.Decode(&cps); err != nil {
				return err
			}
			b.addPushSecret(o, "", cps.Spec.PushSecretSpec)
			return nil
		}
		var ps eso.PushSecret
		if err := o.Decode(&ps); err != nil {
			return err
		}
		b.addPushSecret(o, o.Namespace, ps.Spec)
		return nil
	case phaseWorkload:
		if o.Kind == "Ingress" {
			return b.addIngress(o)
		}
		return b.addWorkload(o)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Node bookkeeping

func (b *builder) node(id string, init func() *Node) *Node {
	if n, ok := b.nodes[id]; ok {
		return n
	}
	n := init()
	n.ID = id
	if n.Attributes == nil {
		n.Attributes = map[string]string{}
	}
	b.nodes[id] = n
	b.order = append(b.order, id)
	return n
}

func (b *builder) source(o manifest.Object) *Source {
	return &Source{File: b.relPath(o.File), Line: o.Line, EmbeddedIn: o.EmbeddedIn}
}

func (b *builder) relPath(file string) string {
	for _, root := range b.roots {
		rel, err := filepath.Rel(root, file)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if len(b.roots) > 1 {
			rel = filepath.Join(filepath.Base(root), rel)
		}
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(file)
}

func (b *builder) edge(e Edge) {
	if e.From == "" || e.To == "" {
		return
	}
	if _, dup := b.edgeSet[e]; dup {
		return
	}
	b.edgeSet[e] = struct{}{}
	b.edges = append(b.edges, e)
}

func (b *builder) diag(sev Severity, nodeID string, o manifest.Object, format string, args ...any) {
	d := Diagnostic{Severity: sev, Message: fmt.Sprintf(format, args...), Node: nodeID}
	if o.File != "" {
		d.File = b.relPath(o.File)
	}
	b.diags = append(b.diags, d)
}

// find returns the first node of the given type (and kind, if non-empty)
// with a matching name and compatible namespace. An exact namespace match
// wins; otherwise an empty namespace on either side is treated as a
// wildcard, since many repositories leave namespaces to kustomize or Helm.
func (b *builder) find(t NodeType, kind, ns, name string) *Node {
	var loose *Node
	for _, id := range b.order {
		n := b.nodes[id]
		if n.Type != t || n.Name != name || (kind != "" && n.Kind != kind) {
			continue
		}
		if n.Namespace == ns {
			return n
		}
		if loose == nil && (n.Namespace == "" || ns == "") {
			loose = n
		}
	}
	return loose
}

func qualified(ns, name string) string {
	if ns == "" {
		return name
	}
	return ns + "/" + name
}

// ---------------------------------------------------------------------------
// Stores and remote keys

func storeID(kind, ns, name string) string {
	if kind == eso.KindClusterSecretStore {
		ns = ""
	}
	return "store:" + kind + "/" + qualified(ns, name)
}

func (b *builder) addStore(o manifest.Object) error {
	var s eso.SecretStore
	if err := o.Decode(&s); err != nil {
		return err
	}
	ns := o.Namespace
	if o.Kind == eso.KindClusterSecretStore {
		ns = ""
	}
	n := b.node(storeID(o.Kind, ns, o.Name), func() *Node {
		return &Node{Type: NodeStore, Kind: o.Kind, Name: o.Name, Namespace: ns, Label: o.Name}
	})
	n.Source = b.source(o)
	n.Missing = false

	provider, cfg := firstProvider(s.Spec.Provider)
	n.Provider = provider
	for k, v := range scalarAttrs(cfg) {
		n.Attributes[k] = v
	}
	if auth, ok := cfg["auth"].(map[string]any); ok {
		n.Attributes["auth"] = strings.Join(sortedKeys(auth), ", ")
	}
	if s.Spec.Controller != "" {
		n.Attributes["controller"] = s.Spec.Controller
	}
	n.Attributes["backend"] = backendName(provider, n.Attributes)
	b.labels[n.ID] = o.Labels
	return nil
}

// resolveStore finds the store referenced from an object in namespace ns,
// creating a placeholder node when the store is not defined in any file.
func (b *builder) resolveStore(ref *eso.StoreRef, ns string, from manifest.Object, fromID string) *Node {
	if ref == nil || ref.Name == "" {
		b.diag(SeverityWarning, fromID, from, "%s %s has an entry without a secret store reference", from.Kind, qualified(from.Namespace, from.Name))
		return nil
	}
	kind := ref.Kind
	if kind == "" {
		kind = eso.KindSecretStore
	}
	if kind == eso.KindClusterSecretStore {
		ns = ""
	}
	if n := b.find(NodeStore, kind, ns, ref.Name); n != nil {
		return n
	}
	b.diag(SeverityWarning, fromID, from, "%s %s references %s %q which is not defined in the scanned files",
		from.Kind, qualified(from.Namespace, from.Name), kind, ref.Name)
	return b.node(storeID(kind, ns, ref.Name), func() *Node {
		return &Node{Type: NodeStore, Kind: kind, Name: ref.Name, Namespace: ns, Label: ref.Name, Missing: true}
	})
}

// remote returns the node representing key inside store, linking it to the
// store with a contains edge.
func (b *builder) remote(store *Node, key, label string) *Node {
	n := b.node("remote:"+store.ID+"|"+key, func() *Node {
		attrs := map[string]string{
			"store":    store.Kind + " " + qualified(store.Namespace, store.Name),
			"key":      key,
			"fullPath": remotePath(store, key),
		}
		if label == "" {
			label = attrs["fullPath"]
		}
		return &Node{
			Type: NodeRemote, Kind: "RemoteSecret", Name: key, Namespace: store.Namespace,
			Label: label, Provider: store.Provider, Attributes: attrs,
		}
	})
	b.edge(Edge{From: store.ID, To: n.ID, Type: EdgeContains})
	return n
}

// remoteItem adds the item for property (or nothing, for the whole value)
// and returns its ID.
func remoteItem(n *Node, property, version string) string {
	if property == "" {
		return ""
	}
	id := "prop:" + property
	it := Item{ID: id, Label: property}
	if version != "" {
		it.Detail = "version " + version
	}
	n.AddItem(it)
	return id
}

func remotePath(store *Node, key string) string {
	mount := strings.Trim(store.Attributes["path"], "/")
	if (store.Provider == "vault" || store.Provider == "openbao") && mount != "" && !strings.HasPrefix(key, mount+"/") {
		return mount + "/" + strings.TrimPrefix(key, "/")
	}
	return key
}

func firstProvider(p map[string]map[string]any) (string, map[string]any) {
	for _, k := range sortedKeys(p) {
		return k, p[k]
	}
	return "", nil
}

// skippedAttrs are provider fields that are noisy (or may be sensitive)
// and therefore never copied into node attributes.
var skippedAttrs = map[string]bool{"caBundle": true, "caProvider": true, "token": true, "password": true}

// scalarAttrs returns the top-level scalar values of m as strings.
func scalarAttrs(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		if skippedAttrs[k] {
			continue
		}
		switch v := v.(type) {
		case string, bool, int, int64, float64:
			out[k] = fmt.Sprint(v)
		case []any:
			var parts []string
			for _, e := range v {
				switch e.(type) {
				case string, bool, int, int64, float64:
					parts = append(parts, fmt.Sprint(e))
				}
			}
			if len(parts) == len(v) && len(parts) > 0 {
				out[k] = strings.Join(parts, ", ")
			}
		}
	}
	return out
}

var backendNames = map[string]string{
	"vault":                   "HashiCorp Vault",
	"openbao":                 "OpenBao",
	"aws":                     "AWS",
	"gcpsm":                   "GCP Secret Manager",
	"azurekv":                 "Azure Key Vault",
	"kubernetes":              "Kubernetes",
	"onepassword":             "1Password",
	"onepasswordSDK":          "1Password",
	"akeyless":                "Akeyless",
	"doppler":                 "Doppler",
	"infisical":               "Infisical",
	"bitwardensecretsmanager": "Bitwarden",
	"gitlab":                  "GitLab",
	"ibm":                     "IBM Secrets Manager",
	"conjur":                  "CyberArk Conjur",
	"yandexlockbox":           "Yandex Lockbox",
	"oracle":                  "Oracle Vault",
	"alibaba":                 "Alibaba KMS",
	"webhook":                 "Webhook",
	"fake":                    "Fake",
	"pulumi":                  "Pulumi ESC",
	"passbolt":                "Passbolt",
	"keepersecurity":          "Keeper",
	"scaleway":                "Scaleway",
	"delinea":                 "Delinea",
	"senhasegura":             "senhasegura",
	"chef":                    "Chef",
	"device42":                "Device42",
	"previder":                "Previder",
	"beyondtrust":             "BeyondTrust",
	"cloudrusm":               "Cloud.ru",
	"github":                  "GitHub",
}

// backendName returns a display name for a provider. The vault provider is
// also used for OpenBao, which is detected from the server URL.
func backendName(provider string, attrs map[string]string) string {
	if provider == "vault" && strings.Contains(strings.ToLower(attrs["server"]), "bao") {
		return "OpenBao"
	}
	if provider == "aws" && attrs["service"] != "" {
		return "AWS " + attrs["service"]
	}
	if name, ok := backendNames[provider]; ok {
		return name
	}
	return provider
}

// ---------------------------------------------------------------------------
// Generators

func generatorID(kind, ns, name string) string {
	return "generator:" + kind + "/" + qualified(ns, name)
}

func (b *builder) addGenerator(o manifest.Object) error {
	var g eso.Generator
	if err := o.Decode(&g); err != nil {
		return err
	}
	ns := o.Namespace
	if o.Kind == eso.KindClusterGenerator {
		ns = ""
	}
	n := b.node(generatorID(o.Kind, ns, o.Name), func() *Node {
		return &Node{Type: NodeGenerator, Kind: o.Kind, Name: o.Name, Namespace: ns, Label: o.Name}
	})
	n.Source = b.source(o)
	n.Missing = false
	for k, v := range scalarAttrs(g.Spec) {
		n.Attributes[k] = v
	}
	if p, ok := g.Spec["provider"].(map[string]any); ok {
		for k, v := range scalarAttrs(p) {
			n.Attributes["provider."+k] = v
		}
	}
	return nil
}

func (b *builder) resolveGenerator(ref *eso.GeneratorRef, ns string, from manifest.Object, fromID string) *Node {
	if ref.Kind == eso.KindClusterGenerator {
		ns = ""
	}
	if n := b.find(NodeGenerator, ref.Kind, ns, ref.Name); n != nil {
		return n
	}
	b.diag(SeverityWarning, fromID, from, "%s %s references generator %s %q which is not defined in the scanned files",
		from.Kind, qualified(from.Namespace, from.Name), ref.Kind, ref.Name)
	return b.node(generatorID(ref.Kind, ns, ref.Name), func() *Node {
		return &Node{Type: NodeGenerator, Kind: ref.Kind, Name: ref.Name, Namespace: ns, Label: ref.Name, Missing: true}
	})
}

// ---------------------------------------------------------------------------
// Kubernetes Secrets

func secretID(ns, name string) string { return "secret:" + qualified(ns, name) }

func keyItem(key string) string { return "key:" + key }

func (b *builder) addSecretManifest(o manifest.Object) error {
	var s eso.Secret
	if err := o.Decode(&s); err != nil {
		return err
	}
	n := b.ensureSecret(o.Namespace, o.Name)
	n.Source = b.source(o)
	n.Missing = false
	if s.Type != "" {
		n.Attributes["type"] = s.Type
	}
	keys := sortedKeys(s.Data)
	keys = append(keys, sortedKeys(s.StringData)...)
	for _, k := range keys {
		n.AddItem(Item{ID: keyItem(k), Label: k, Detail: "defined in manifest"})
	}
	return nil
}

// ensureSecret returns the Secret node for ns/name, creating it if needed.
func (b *builder) ensureSecret(ns, name string) *Node {
	if n := b.find(NodeSecret, "", ns, name); n != nil {
		return n
	}
	return b.node(secretID(ns, name), func() *Node {
		return &Node{Type: NodeSecret, Kind: "Secret", Name: name, Namespace: ns, Label: name}
	})
}

// consumedSecret resolves a Secret referenced by a consumer, marking it as
// missing when nothing in the scanned files defines or manages it.
func (b *builder) consumedSecret(ns, name string) *Node {
	if n := b.find(NodeSecret, "", ns, name); n != nil {
		return n
	}
	return b.node(secretID(ns, name), func() *Node {
		return &Node{Type: NodeSecret, Kind: "Secret", Name: name, Namespace: ns, Label: name, Missing: true}
	})
}

// secretKey returns the item of secret that provides key, adding it when
// necessary and warning when a managed secret does not provide it.
func (b *builder) secretKey(secret *Node, key string, from manifest.Object, fromID string) string {
	id := keyItem(key)
	switch {
	case secret.HasItem(id):
		return id
	case secret.HasItem(WildcardItem):
		// Provided by dataFrom or templateFrom; key names are only known at runtime.
		return WildcardItem
	case secret.Missing:
		secret.AddItem(Item{ID: id, Label: key})
		return id
	}
	secret.AddItem(Item{ID: id, Label: key, Detail: "not provided by any scanned resource"})
	b.diag(SeverityWarning, fromID, from, "%s %s uses key %q of Secret %s, but no scanned resource provides that key",
		from.Kind, qualified(from.Namespace, from.Name), key, qualified(secret.Namespace, secret.Name))
	return id
}

func (b *builder) reportSecrets() {
	out := map[string]bool{}
	synced := map[string]bool{}
	for _, e := range b.edges {
		out[e.From] = true
		if e.Type == EdgeSync {
			synced[e.To] = true
		}
	}
	for _, id := range b.order {
		n := b.nodes[id]
		if n.Type == NodeSecret && n.Missing {
			b.diags = append(b.diags, Diagnostic{
				Severity: SeverityInfo,
				Node:     id,
				Message:  fmt.Sprintf("Secret %s is used but neither managed by ESO nor defined in the scanned files", qualified(n.Namespace, n.Name)),
			})
		}
		if n.Type == NodeSecret && synced[id] && !out[id] {
			b.diags = append(b.diags, Diagnostic{
				Severity: SeverityInfo,
				Node:     id,
				Message:  fmt.Sprintf("Secret %s is synced but not referenced by any scanned workload or PushSecret", qualified(n.Namespace, n.Name)),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var typeOrder = map[NodeType]int{
	NodeStore: 0, NodeGenerator: 1, NodeRemote: 2, NodeExternalSecret: 3,
	NodePushSecret: 4, NodeSecret: 5, NodeWorkload: 6,
}

func sortGraph(g *Graph) {
	sort.SliceStable(g.Nodes, func(i, j int) bool {
		a, c := g.Nodes[i], g.Nodes[j]
		if typeOrder[a.Type] != typeOrder[c.Type] {
			return typeOrder[a.Type] < typeOrder[c.Type]
		}
		if a.Namespace != c.Namespace {
			return a.Namespace < c.Namespace
		}
		return a.ID < c.ID
	})
	sort.SliceStable(g.Edges, func(i, j int) bool {
		a, c := g.Edges[i], g.Edges[j]
		if a.From != c.From {
			return a.From < c.From
		}
		if a.FromItem != c.FromItem {
			return a.FromItem < c.FromItem
		}
		if a.To != c.To {
			return a.To < c.To
		}
		return a.ToItem < c.ToItem
	})
}
