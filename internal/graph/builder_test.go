package graph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vincent/eso-map/internal/graph"
	"github.com/vincent/eso-map/internal/manifest"
)

func buildExamples(t *testing.T) *graph.Graph {
	t.Helper()
	const root = "../../examples"
	files, err := manifest.Discover(root, manifest.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	objs, errs := manifest.LoadFiles(files)
	if len(errs) > 0 {
		t.Fatalf("LoadFiles: %v", errs)
	}
	return graph.Build([]string{root}, objs)
}

func buildYAML(t *testing.T, doc string) *graph.Graph {
	t.Helper()
	objs, err := manifest.Parse("test.yaml", []byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return graph.Build([]string{"."}, objs)
}

func hasFlow(g *graph.Graph, parts ...string) bool {
	for _, f := range g.Flows {
		ok := true
		for _, p := range parts {
			if !strings.Contains(f.Summary, p) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func node(t *testing.T, g *graph.Graph, id string) *graph.Node {
	t.Helper()
	for _, n := range g.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found", id)
	return nil
}

func TestBuildExamplesFlows(t *testing.T) {
	g := buildExamples(t)

	tests := []struct {
		name  string
		parts []string
	}{
		{
			name: "openbao property to templated env var",
			parts: []string{
				"OpenBao (ClusterSecretStore openbao)",
				"secret/apps/api/database#password",
				"ExternalSecret api/api-db [password]",
				"Secret api/api-db-credentials [DATABASE_URL]",
				"Deployment api/api [$DATABASE_URL]",
			},
		},
		{
			name:  "aws to statefulset",
			parts: []string{"AWS SecretsManager", "prod/payments/stripe#api_key", "StatefulSet payments/payments [$STRIPE_API_KEY]"},
		},
		{
			name:  "generator to push secret back into openbao",
			parts: []string{"Password api/strong-password", "Secret api/api-session", "PushSecret api/api-session-backup", "secret/apps/api/session#key"},
		},
		{
			name:  "dataFrom extract into envFrom",
			parts: []string{"secret/apps/worker/env", "CronJob worker/nightly-report [envFrom worker-env]"},
		},
		{
			name:  "tls secret mounted and used by ingress",
			parts: []string{"#private_key", "Secret api/api-tls [tls.key]", "Deployment api/api [/etc/tls]"},
		},
		{
			name:  "ingress tls",
			parts: []string{"Secret api/api-tls", "Ingress api/api [TLS api.example.com]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !hasFlow(g, tt.parts...) {
				var all []string
				for _, f := range g.Flows {
					all = append(all, f.Summary)
				}
				t.Errorf("no flow contains %q; flows:\n%s", tt.parts, strings.Join(all, "\n"))
			}
		})
	}
}

func TestBuildExamplesDiagnostics(t *testing.T) {
	g := buildExamples(t)
	want := []string{
		`uses key "STRIPE_PUBLISHABLE_KEY" of Secret payments/stripe-keys`,
		"Secret worker/smtp-credentials is used but neither managed by ESO",
		"Secret api/registry-credentials is used but neither managed by ESO",
	}
	for _, w := range want {
		found := slices.ContainsFunc(g.Diagnostics, func(d graph.Diagnostic) bool { return strings.Contains(d.Message, w) })
		if !found {
			t.Errorf("missing diagnostic containing %q; got %+v", w, g.Diagnostics)
		}
	}
}

func TestBuildStoreAttributes(t *testing.T) {
	g := buildExamples(t)
	s := node(t, g, "store:ClusterSecretStore/openbao")
	if s.Provider != "vault" || s.Attributes["backend"] != "OpenBao" {
		t.Errorf("provider=%q backend=%q, want vault/OpenBao", s.Provider, s.Attributes["backend"])
	}
	if s.Attributes["auth"] != "kubernetes" || s.Attributes["path"] != "secret" {
		t.Errorf("unexpected attributes %v", s.Attributes)
	}
	if s.Source == nil || s.Source.File != "platform/stores.yaml" || s.Source.Line != 1 {
		t.Errorf("source = %+v", s.Source)
	}
}

func TestMissingStoreIsReported(t *testing.T) {
	g := buildYAML(t, `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata: {name: es, namespace: ns}
spec:
  secretStoreRef: {name: nowhere}
  data:
    - secretKey: k
      remoteRef: {key: a/b}
`)
	s := node(t, g, "store:SecretStore/ns/nowhere")
	if !s.Missing {
		t.Error("store should be marked missing")
	}
	if len(g.Diagnostics) == 0 || !strings.Contains(g.Diagnostics[0].Message, `SecretStore "nowhere"`) {
		t.Errorf("diagnostics = %+v", g.Diagnostics)
	}
}

func TestNamespaceLessManifestsMatch(t *testing.T) {
	// Namespaces are often injected by kustomize; references must still resolve.
	g := buildYAML(t, `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata: {name: es}
spec:
  secretStoreRef: {name: store, kind: ClusterSecretStore}
  target: {name: creds}
  data:
    - secretKey: token
      remoteRef: {key: x}
---
apiVersion: v1
kind: Pod
metadata: {name: p, namespace: team}
spec:
  containers:
    - name: c
      env:
        - name: TOKEN
          valueFrom: {secretKeyRef: {name: creds, key: token}}
`)
	if !hasFlow(g, "ExternalSecret es [token]", "Secret creds [token]", "Pod team/p [$TOKEN]") {
		t.Errorf("expected flow across namespace-less manifests, got %+v", g.Flows)
	}
}

func TestTemplateMergePolicy(t *testing.T) {
	g := buildYAML(t, `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata: {name: es, namespace: ns}
spec:
  secretStoreRef: {name: s, kind: ClusterSecretStore}
  target:
    template:
      mergePolicy: Merge
      data:
        conn: '{{ index . "db-pass" }}@host'
  data:
    - secretKey: db-pass
      remoteRef: {key: k}
`)
	sec := node(t, g, "secret:ns/es")
	for _, id := range []string{"key:conn", "key:db-pass"} {
		if !sec.HasItem(id) {
			t.Errorf("secret missing item %q: %+v", id, sec.Items)
		}
	}
}
