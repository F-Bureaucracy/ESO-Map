package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Object is a single Kubernetes object decoded from a YAML document.
type Object struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	Labels     map[string]string

	// File and Line locate the document the object was read from.
	File string
	Line int
	// EmbeddedIn describes the enclosing object for objects found nested
	// inside another one (resource templates of composition tools and
	// operators), e.g. "ResourceGraphDefinition application#dbPassword".
	// Empty for top-level objects.
	EmbeddedIn string

	node *yaml.Node
}

// Group returns the API group of the object ("" for the core group).
func (o Object) Group() string {
	group, _, found := strings.Cut(o.APIVersion, "/")
	if !found {
		return ""
	}
	return group
}

// Decode unmarshals the full object into v.
func (o Object) Decode(v any) error {
	if o.node == nil {
		return errors.New("object has no backing YAML node")
	}
	return o.node.Decode(v)
}

// ParseError records a YAML document that could not be decoded.
type ParseError struct {
	File string
	Err  error
}

func (e ParseError) Error() string { return fmt.Sprintf("%s: %v", e.File, e.Err) }
func (e ParseError) Unwrap() error { return e.Err }

type header struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
	Items []yaml.Node `yaml:"items"`
}

// LoadFiles reads every file and returns the Kubernetes objects found.
// Documents that are not Kubernetes objects (no apiVersion/kind) are ignored.
// Files that fail to parse (for example un-rendered Helm templates) are
// reported as ParseErrors and do not abort loading.
func LoadFiles(files []string) ([]Object, []ParseError) {
	var (
		objs []Object
		errs []ParseError
	)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, ParseError{File: f, Err: err})
			continue
		}
		o, err := Parse(f, data)
		objs = append(objs, o...)
		if err != nil {
			errs = append(errs, ParseError{File: f, Err: err})
		}
	}
	return objs, errs
}

// Parse decodes all YAML documents in data. The objects decoded before an
// error is encountered are returned together with that error.
func Parse(file string, data []byte) ([]Object, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var objs []Object
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return objs, err
		}
		if len(doc.Content) == 0 {
			continue
		}
		found, err := fromNode(file, doc.Content[0])
		if err != nil {
			return objs, err
		}
		objs = append(objs, found...)
	}
}

func fromNode(file string, n *yaml.Node) ([]Object, error) {
	if n.Kind != yaml.MappingNode {
		return nil, nil
	}
	var h header
	if err := n.Decode(&h); err != nil {
		return nil, fmt.Errorf("line %d: %w", n.Line, err)
	}
	if h.APIVersion == "" || h.Kind == "" {
		return nil, nil
	}
	// Expand v1/List (e.g. output of `kubectl get -o yaml`).
	if strings.HasSuffix(h.Kind, "List") && h.Items != nil {
		var objs []Object
		for i := range h.Items {
			found, err := fromNode(file, &h.Items[i])
			if err != nil {
				return objs, err
			}
			objs = append(objs, found...)
		}
		return objs, nil
	}
	top := Object{
		APIVersion: h.APIVersion,
		Kind:       h.Kind,
		Name:       h.Metadata.Name,
		Namespace:  h.Metadata.Namespace,
		Labels:     h.Metadata.Labels,
		File:       file,
		Line:       n.Line,
		node:       n,
	}
	resolveIDReferences(n)
	objs := []Object{top}
	var walkErr error
	walkEmbedded(n, top.Kind+" "+top.Name, func(child *yaml.Node, via string) {
		if walkErr != nil {
			return
		}
		var ch header
		if err := child.Decode(&ch); err != nil {
			walkErr = fmt.Errorf("line %d: %w", child.Line, err)
			return
		}
		objs = append(objs, Object{
			APIVersion: ch.APIVersion,
			Kind:       ch.Kind,
			Name:       ch.Metadata.Name,
			Namespace:  ch.Metadata.Namespace,
			Labels:     ch.Metadata.Labels,
			File:       file,
			Line:       child.Line,
			EmbeddedIn: via,
			node:       child,
		})
	})
	return objs, walkErr
}
