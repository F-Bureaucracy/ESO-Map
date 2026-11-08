// Package manifest discovers YAML files on disk and decodes the Kubernetes
// objects they contain.
package manifest

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

// DefaultIncludes are the file name patterns scanned when none are given.
var DefaultIncludes = []string{"*.yaml", "*.yml"}

// DefaultExcludes are directory and file patterns skipped when none are given.
var DefaultExcludes = []string{".git", "node_modules", "vendor", ".terraform"}

// DiscoverOptions controls which files Discover returns.
type DiscoverOptions struct {
	// Include holds glob patterns (filepath.Match syntax). A file is included
	// when its base name or its slash-separated path relative to the root
	// matches any pattern.
	Include []string
	// Exclude holds glob patterns matched the same way. Matching directories
	// are not descended into.
	Exclude []string
}

// Discover walks root and returns the sorted list of files matching opts.
// If root is a regular file it is returned as-is.
func Discover(root string, opts DiscoverOptions) ([]string, error) {
	if len(opts.Include) == 0 {
		opts.Include = DefaultIncludes
	}
	for _, p := range append(append([]string{}, opts.Include...), opts.Exclude...) {
		if _, err := filepath.Match(p, ""); err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", p, err)
		}
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		if rel != "." && matchesAny(opts.Exclude, d.Name(), rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if path == root || matchesAny(opts.Include, d.Name(), rel) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}

func matchesAny(patterns []string, name, rel string) bool {
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, name); ok {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}
