// Package graph models how secrets flow between secret providers, External
// Secrets Operator resources, Kubernetes Secrets and the workloads that
// consume them.
package graph

// NodeType classifies a node in the graph.
type NodeType string

// Node types, roughly ordered from the external provider to the consumer.
const (
	NodeStore          NodeType = "store"          // SecretStore / ClusterSecretStore
	NodeGenerator      NodeType = "generator"      // generators.external-secrets.io
	NodeRemote         NodeType = "remote"         // a key/path inside a provider
	NodeExternalSecret NodeType = "externalsecret" // ExternalSecret / ClusterExternalSecret
	NodePushSecret     NodeType = "pushsecret"     // PushSecret / ClusterPushSecret
	NodeSecret         NodeType = "secret"         // Kubernetes Secret
	NodeWorkload       NodeType = "workload"       // Deployment, CronJob, Ingress, ...
)

// EdgeType describes the relationship an edge represents.
type EdgeType string

// Edge types.
const (
	EdgeContains EdgeType = "contains" // store -> remote key it hosts
	EdgeFetch    EdgeType = "fetch"    // remote key -> ExternalSecret entry
	EdgeGenerate EdgeType = "generate" // generator -> ExternalSecret/PushSecret entry
	EdgeSync     EdgeType = "sync"     // ExternalSecret entry -> Secret key
	EdgeSelect   EdgeType = "select"   // Secret key -> PushSecret entry
	EdgePush     EdgeType = "push"     // PushSecret entry -> remote key
	EdgeConsume  EdgeType = "consume"  // Secret key -> workload env var / mount
)

// WildcardItem is the item ID used for keys that are only known at runtime,
// such as everything produced by dataFrom.
const WildcardItem = "*"

// Graph is the full secret topology discovered in a set of manifests.
type Graph struct {
	Roots       []string     `json:"roots" yaml:"roots"`
	Files       int          `json:"files" yaml:"files"`
	Nodes       []*Node      `json:"nodes" yaml:"nodes"`
	Edges       []Edge       `json:"edges" yaml:"edges"`
	Flows       []Flow       `json:"flows" yaml:"flows"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty" yaml:"diagnostics,omitempty"`
}

// Node is a resource or a remote secret location.
type Node struct {
	ID        string   `json:"id" yaml:"id"`
	Type      NodeType `json:"type" yaml:"type"`
	Kind      string   `json:"kind" yaml:"kind"`
	Name      string   `json:"name" yaml:"name"`
	Namespace string   `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// Label is a short human readable title.
	Label string `json:"label" yaml:"label"`
	// Provider is the secret backend, e.g. "vault", "aws" (stores and remote keys).
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	// Attributes holds notable, non-sensitive configuration values.
	Attributes map[string]string `json:"attributes,omitempty" yaml:"attributes,omitempty"`
	// Items are the addressable parts of a node (secret keys, env vars, ...).
	Items []Item `json:"items,omitempty" yaml:"items,omitempty"`
	// Source points at the manifest defining the node; nil for derived nodes.
	Source *Source `json:"source,omitempty" yaml:"source,omitempty"`
	// Missing is true for nodes that are referenced but not defined in any
	// scanned file (e.g. a Secret created by hand).
	Missing bool `json:"missing,omitempty" yaml:"missing,omitempty"`
}

// Item is an addressable part of a node.
type Item struct {
	ID     string `json:"id" yaml:"id"`
	Label  string `json:"label" yaml:"label"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// Source locates a node's definition.
type Source struct {
	File string `json:"file" yaml:"file"`
	Line int    `json:"line" yaml:"line"`
	// EmbeddedIn names the enclosing object for definitions nested inside
	// another object, such as a resource template inside a composition CRD.
	EmbeddedIn string `json:"embeddedIn,omitempty" yaml:"embeddedIn,omitempty"`
}

// Edge connects two nodes, optionally at item granularity. An empty item
// means the edge applies to the node as a whole.
type Edge struct {
	From     string   `json:"from" yaml:"from"`
	FromItem string   `json:"fromItem,omitempty" yaml:"fromItem,omitempty"`
	To       string   `json:"to" yaml:"to"`
	ToItem   string   `json:"toItem,omitempty" yaml:"toItem,omitempty"`
	Type     EdgeType `json:"type" yaml:"type"`
}

// Hop is one step of a Flow.
type Hop struct {
	Node string `json:"node" yaml:"node"`
	Item string `json:"item,omitempty" yaml:"item,omitempty"`
}

// Flow is an end-to-end path a secret value takes, e.g. from an OpenBao path
// through an ExternalSecret and a Kubernetes Secret into an env var.
type Flow struct {
	Summary string `json:"summary" yaml:"summary"`
	Hops    []Hop  `json:"hops" yaml:"hops"`
}

// Severity of a Diagnostic.
type Severity string

// Diagnostic severities.
const (
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Diagnostic reports a likely misconfiguration or notable fact.
type Diagnostic struct {
	Severity Severity `json:"severity" yaml:"severity"`
	Message  string   `json:"message" yaml:"message"`
	Node     string   `json:"node,omitempty" yaml:"node,omitempty"`
	File     string   `json:"file,omitempty" yaml:"file,omitempty"`
}

// HasItem reports whether n has an item with the given ID.
func (n *Node) HasItem(id string) bool {
	for _, it := range n.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// AddItem appends an item unless one with the same ID exists.
func (n *Node) AddItem(it Item) {
	if !n.HasItem(it.ID) {
		n.Items = append(n.Items, it)
	}
}

// Item returns the item with the given ID.
func (n *Node) Item(id string) (Item, bool) {
	for _, it := range n.Items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}
