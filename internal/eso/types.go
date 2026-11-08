// Package eso contains minimal, decode-only representations of the External
// Secrets Operator CRDs and the parts of core Kubernetes resources needed to
// map secret usage. Only fields relevant for mapping are modelled, which
// keeps the tool independent of ESO's (large) Go module and tolerant of
// API version differences (v1alpha1, v1beta1, v1).
package eso

// API groups.
const (
	Group          = "external-secrets.io"
	GeneratorGroup = "generators.external-secrets.io"
)

// Kinds.
const (
	KindSecretStore           = "SecretStore"
	KindClusterSecretStore    = "ClusterSecretStore"
	KindExternalSecret        = "ExternalSecret"
	KindClusterExternalSecret = "ClusterExternalSecret"
	KindPushSecret            = "PushSecret"
	KindClusterPushSecret     = "ClusterPushSecret"
	KindClusterGenerator      = "ClusterGenerator"
)

// SecretStore covers SecretStore and ClusterSecretStore.
type SecretStore struct {
	Spec struct {
		// Provider has exactly one key naming the backend (vault, aws, ...).
		Provider   map[string]map[string]any `yaml:"provider"`
		Controller string                    `yaml:"controller"`
	} `yaml:"spec"`
}

// StoreRef references a SecretStore or ClusterSecretStore.
type StoreRef struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
}

// GeneratorRef references a generator resource.
type GeneratorRef struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Name       string `yaml:"name"`
}

// SourceRef overrides the store of a single data entry or selects a generator.
type SourceRef struct {
	StoreRef     *StoreRef     `yaml:"storeRef"`
	GeneratorRef *GeneratorRef `yaml:"generatorRef"`
}

// RemoteRef addresses a value in the provider.
type RemoteRef struct {
	Key      string `yaml:"key"`
	Property string `yaml:"property"`
	Version  string `yaml:"version"`
}

// Find selects many provider secrets by name/path/tags.
type Find struct {
	Path string `yaml:"path"`
	Name *struct {
		RegExp string `yaml:"regexp"`
	} `yaml:"name"`
	Tags map[string]string `yaml:"tags"`
}

// Template shapes the resulting Kubernetes Secret.
type Template struct {
	Type         string            `yaml:"type"`
	Data         map[string]string `yaml:"data"`
	MergePolicy  string            `yaml:"mergePolicy"`
	TemplateFrom []any             `yaml:"templateFrom"`
}

// ExternalSecretSpec is the spec of an ExternalSecret.
type ExternalSecretSpec struct {
	SecretStoreRef  *StoreRef `yaml:"secretStoreRef"`
	RefreshInterval string    `yaml:"refreshInterval"`
	Target          struct {
		Name           string    `yaml:"name"`
		CreationPolicy string    `yaml:"creationPolicy"`
		Template       *Template `yaml:"template"`
	} `yaml:"target"`
	Data []struct {
		SecretKey string     `yaml:"secretKey"`
		RemoteRef RemoteRef  `yaml:"remoteRef"`
		SourceRef *SourceRef `yaml:"sourceRef"`
	} `yaml:"data"`
	DataFrom []struct {
		Extract   *RemoteRef `yaml:"extract"`
		Find      *Find      `yaml:"find"`
		SourceRef *SourceRef `yaml:"sourceRef"`
		Rewrite   []any      `yaml:"rewrite"`
	} `yaml:"dataFrom"`
}

// ExternalSecret is an external-secrets.io ExternalSecret.
type ExternalSecret struct {
	Spec ExternalSecretSpec `yaml:"spec"`
}

// ClusterExternalSecret stamps an ExternalSecret into many namespaces.
type ClusterExternalSecret struct {
	Spec struct {
		ExternalSecretName string             `yaml:"externalSecretName"`
		Namespaces         []string           `yaml:"namespaces"`
		ExternalSecretSpec ExternalSecretSpec `yaml:"externalSecretSpec"`
	} `yaml:"spec"`
}

// PushSecretSpec is the spec of a PushSecret.
type PushSecretSpec struct {
	SecretStoreRefs []struct {
		Name          string         `yaml:"name"`
		Kind          string         `yaml:"kind"`
		LabelSelector *LabelSelector `yaml:"labelSelector"`
	} `yaml:"secretStoreRefs"`
	Selector struct {
		Secret *struct {
			Name string `yaml:"name"`
		} `yaml:"secret"`
		GeneratorRef *GeneratorRef `yaml:"generatorRef"`
	} `yaml:"selector"`
	Data []struct {
		Match struct {
			SecretKey string `yaml:"secretKey"`
			RemoteRef struct {
				RemoteKey string `yaml:"remoteKey"`
				Property  string `yaml:"property"`
			} `yaml:"remoteRef"`
		} `yaml:"match"`
	} `yaml:"data"`
	UpdatePolicy   string `yaml:"updatePolicy"`
	DeletionPolicy string `yaml:"deletionPolicy"`
}

// PushSecret is an external-secrets.io PushSecret.
type PushSecret struct {
	Spec PushSecretSpec `yaml:"spec"`
}

// ClusterPushSecret stamps a PushSecret into many namespaces.
type ClusterPushSecret struct {
	Spec struct {
		PushSecretName string         `yaml:"pushSecretName"`
		PushSecretSpec PushSecretSpec `yaml:"pushSecretSpec"`
	} `yaml:"spec"`
}

// LabelSelector is a Kubernetes label selector (matchLabels only).
type LabelSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels"`
}

// Matches reports whether labels satisfy the selector's matchLabels.
func (s *LabelSelector) Matches(labels map[string]string) bool {
	if s == nil {
		return false
	}
	for k, v := range s.MatchLabels {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// Generator is any generators.external-secrets.io resource.
type Generator struct {
	Spec map[string]any `yaml:"spec"`
}

// Secret is a core/v1 Secret. Only key names are read; values are never kept.
type Secret struct {
	Type       string         `yaml:"type"`
	Data       map[string]any `yaml:"data"`
	StringData map[string]any `yaml:"stringData"`
}
