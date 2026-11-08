package graph

import (
	"slices"
	"testing"
)

func TestTemplateRefs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"{{ .user }}:{{ .password | b64dec }}", []string{"user", "password"}},
		{`{{ index . "tls.crt" }}`, []string{"tls.crt"}},
		{"plain text", nil},
		{"{{ .a }}{{ .a }}", []string{"a"}},
		{"{{- if .enabled -}}{{ .value }}{{- end }}", []string{"enabled", "value"}},
	}
	for _, tt := range tests {
		if got := templateRefs(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("templateRefs(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRemotePath(t *testing.T) {
	store := &Node{Provider: "vault", Attributes: map[string]string{"path": "secret"}}
	for key, want := range map[string]string{
		"apps/db":        "secret/apps/db",
		"secret/apps/db": "secret/apps/db",
		"/apps/db":       "secret/apps/db",
	} {
		if got := remotePath(store, key); got != want {
			t.Errorf("remotePath(%q) = %q, want %q", key, got, want)
		}
	}
}
