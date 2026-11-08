package manifest

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestParseMultiDocAndList(t *testing.T) {
	data := []byte(`
# leading comment
apiVersion: v1
kind: ConfigMap
metadata: {name: a, namespace: x}
---
---
just: some-config
---
apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: Secret
    metadata: {name: b}
  - apiVersion: apps/v1
    kind: Deployment
    metadata: {name: c}
`)
	objs, err := Parse("f.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range objs {
		got = append(got, o.Kind+"/"+o.Name)
	}
	want := []string{"ConfigMap/a", "Secret/b", "Deployment/c"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if objs[0].Line != 3 || objs[0].Namespace != "x" {
		t.Errorf("line=%d ns=%q", objs[0].Line, objs[0].Namespace)
	}
	if objs[2].Group() != "apps" || objs[1].Group() != "" {
		t.Errorf("groups: %q %q", objs[2].Group(), objs[1].Group())
	}
}

func TestParseHelmTemplateFails(t *testing.T) {
	_, err := Parse("t.yaml", []byte("apiVersion: v1\nkind: Secret\nmetadata:\n  name: {{ .Values.name }}\n  {{- include \"x\" . }}\n"))
	if err == nil {
		t.Fatal("expected an error for un-rendered Helm template")
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.yaml", "b.yml", "c.json", "sub/d.yaml", ".git/e.yaml", "skip/f.yaml"} {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Discover(dir, DiscoverOptions{Exclude: append(DefaultExcludes, "skip")})
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, f := range files {
		r, _ := filepath.Rel(dir, f)
		rel = append(rel, filepath.ToSlash(r))
	}
	want := []string{"a.yaml", "b.yml", "sub/d.yaml"}
	if !slices.Equal(rel, want) {
		t.Errorf("got %v, want %v", rel, want)
	}

	if _, err := Discover(dir, DiscoverOptions{Include: []string{"["}}); err == nil {
		t.Error("expected error for invalid pattern")
	}
}

func TestParseEmbeddedObjects(t *testing.T) {
	// A generic wrapper CRD holding resource templates that reference each
	// other by id.
	data := []byte(`
apiVersion: example.com/v1
kind: Bundle
metadata: {name: app}
spec:
  owner: {apiVersion: v1, kind: Namespace, name: not-an-object}
  resources:
    - id: gen
      template:
        apiVersion: generators.external-secrets.io/v1alpha1
        kind: Password
        metadata: {name: "${schema.name}-db"}
    - id: push
      template:
        apiVersion: external-secrets.io/v1alpha1
        kind: PushSecret
        metadata: {name: push}
        spec:
          selector:
            generatorRef: {kind: Password, name: "${gen.metadata.name}"}
`)
	objs, err := Parse("bundle.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range objs {
		got = append(got, o.Kind+"/"+o.Name+"@"+o.EmbeddedIn)
	}
	want := []string{"Bundle/app@", "Password/${schema.name}-db@Bundle app#gen", "PushSecret/push@Bundle app#push"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	var ps struct {
		Spec struct {
			Selector struct {
				GeneratorRef struct{ Name string } `yaml:"generatorRef"`
			}
		}
	}
	if err := objs[2].Decode(&ps); err != nil {
		t.Fatal(err)
	}
	if n := ps.Spec.Selector.GeneratorRef.Name; n != "${schema.name}-db" {
		t.Errorf("id reference not resolved: %q", n)
	}
}
