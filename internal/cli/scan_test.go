package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := NewRootCommand()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestScanJSON(t *testing.T) {
	stdout, stderr, err := run(t, "scan", "../../examples")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Files int `json:"files"`
		Flows []struct {
			Summary string `json:"summary"`
		} `json:"flows"`
	}
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if g.Files != 6 || len(g.Flows) == 0 {
		t.Errorf("files=%d flows=%d", g.Files, len(g.Flows))
	}
	if !strings.Contains(stderr, "eso-map: 6 files") {
		t.Errorf("summary missing from stderr: %q", stderr)
	}
}

func TestScanHTMLToFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "map.html")
	if _, _, err := run(t, "scan", "../../examples", "-o", "html", "--out", out, "-q"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("<!doctype html>")) {
		t.Errorf("unexpected output start: %q", data[:min(40, len(data))])
	}
}

func TestScanStrict(t *testing.T) {
	_, _, err := run(t, "scan", "../../examples", "--strict", "-q")
	if !errors.Is(err, errWarnings) {
		t.Errorf("err = %v, want errWarnings", err)
	}
}

func TestScanErrors(t *testing.T) {
	if _, _, err := run(t, "scan", "-o", "table"); err == nil {
		t.Error("expected error for unknown format")
	}
	if _, _, err := run(t, "scan", "does-not-exist"); err == nil {
		t.Error("expected error for missing directory")
	}
}
