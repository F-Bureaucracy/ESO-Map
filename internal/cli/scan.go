package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vincent/eso-map/internal/graph"
	"github.com/vincent/eso-map/internal/manifest"
	"github.com/vincent/eso-map/internal/render"
)

type scanOptions struct {
	output  string
	outFile string
	include []string
	exclude []string
	strict  bool
	quiet   bool
}

func newScanCommand() *cobra.Command {
	opts := scanOptions{}
	cmd := &cobra.Command{
		Use:   "scan [directory...]",
		Short: "Scan YAML manifests and render the secret map",
		Example: `  # JSON to stdout for the current directory
  eso-map scan

  # Interactive diagram
  eso-map scan ./deploy -o html --out eso-map.html

  # Several repositories at once, e.g. apps plus the repo defining the stores
  eso-map scan ../deployment ../flux -o html --out eso-map.html

  # Every end-to-end flow, as plain text
  eso-map scan ./deploy -o json | jq -r '.flows[].summary'`,
		Args: cobra.ArbitraryArgs,
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				args = []string{"."}
			}
			return runScan(cmd, args, opts)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.output, "output", "o", string(render.FormatJSON), "output format: json, yaml or html")
	f.StringVar(&opts.outFile, "out", "", "write output to this file instead of stdout")
	f.StringSliceVar(&opts.include, "include", manifest.DefaultIncludes, "glob patterns of files to scan (matched against base name and relative path)")
	f.StringSliceVar(&opts.exclude, "exclude", manifest.DefaultExcludes, "glob patterns of files or directories to skip")
	f.BoolVar(&opts.strict, "strict", false, "exit with an error if any diagnostic warning is reported")
	f.BoolVarP(&opts.quiet, "quiet", "q", false, "do not print the summary and parse errors to stderr")

	_ = cmd.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		var out []string
		for _, f := range render.Formats() {
			out = append(out, string(f))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

// errWarnings is returned in --strict mode when diagnostics contain warnings.
var errWarnings = errors.New("diagnostic warnings reported")

func runScan(cmd *cobra.Command, dirs []string, opts scanOptions) error {
	format, err := render.ParseFormat(opts.output)
	if err != nil {
		return err
	}

	var (
		roots []string
		files []string
	)
	for _, dir := range dirs {
		root, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if _, err := os.Stat(root); err != nil {
			return err
		}
		found, err := manifest.Discover(root, manifest.DiscoverOptions{Include: opts.include, Exclude: opts.exclude})
		if err != nil {
			return err
		}
		roots = append(roots, root)
		files = append(files, found...)
	}
	objs, parseErrs := manifest.LoadFiles(files)

	g := graph.Build(roots, objs)
	g.Files = len(files)

	stderr := cmd.ErrOrStderr()
	if !opts.quiet {
		for _, pe := range parseErrs {
			fmt.Fprintf(stderr, "skipped %v\n", pe)
		}
	}

	if err := writeOutput(cmd.OutOrStdout(), opts.outFile, format, g); err != nil {
		return err
	}

	warnings := 0
	for _, d := range g.Diagnostics {
		if d.Severity == graph.SeverityWarning {
			warnings++
		}
	}
	if !opts.quiet {
		printSummary(stderr, g, warnings, len(parseErrs), opts.outFile)
	}
	if opts.strict && warnings > 0 {
		return fmt.Errorf("%w: %d (see the diagnostics in the output)", errWarnings, warnings)
	}
	return nil
}

func writeOutput(stdout io.Writer, path string, format render.Format, g *graph.Graph) (err error) {
	if path == "" {
		return render.Render(stdout, format, g)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	if err := render.Render(w, format, g); err != nil {
		return err
	}
	return w.Flush()
}

func printSummary(w io.Writer, g *graph.Graph, warnings, parseErrors int, outFile string) {
	counts := map[graph.NodeType]int{}
	for _, n := range g.Nodes {
		counts[n.Type]++
	}
	parts := []string{plural(g.Files, "file")}
	for _, t := range []struct {
		typ   graph.NodeType
		label string
	}{
		{graph.NodeStore, "store"},
		{graph.NodeGenerator, "generator"},
		{graph.NodeRemote, "remote path"},
		{graph.NodeExternalSecret, "external secret"},
		{graph.NodePushSecret, "push secret"},
		{graph.NodeSecret, "secret"},
		{graph.NodeWorkload, "consumer"},
	} {
		if counts[t.typ] > 0 {
			parts = append(parts, plural(counts[t.typ], t.label))
		}
	}
	parts = append(parts, plural(len(g.Flows), "flow"))
	if warnings > 0 {
		parts = append(parts, plural(warnings, "warning"))
	}
	if parseErrors > 0 {
		parts = append(parts, plural(parseErrors, "unparsable file"))
	}
	msg := "eso-map: " + strings.Join(parts, ", ")
	if outFile != "" {
		msg += " → " + outFile
	}
	fmt.Fprintln(w, msg)
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
