package architecture_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// modulePath is this module's import prefix. Only edges within it are checked;
// third-party and standard-library imports are governed by the dependency policy
// (E1-T4) and depguard in .golangci.yml.
const modulePath = "pretty-reasonix"

// layers ranks every package in the module. An import is legal when the target
// sits at the same rank or lower: an upward import is what turns a layered
// design into a cycle, so it fails here rather than in review.
//
// A package added to the tree with no entry here fails TestEveryPackageIsRanked,
// so the rule cannot quietly stop covering new code — which is the failure mode
// that makes architecture tests worth having at all.
var layers = []struct {
	rank  int
	why   string
	paths []string
}{
	{0, "the contract: interfaces and wire types, plus the test that guards them",
		[]string{"internal/contract", "internal/architecture"}},
	{1, "foundations: configuration, storage, safety and small leaf helpers",
		[]string{
			"internal/config", "internal/session", "internal/perm",
			"internal/sandbox", "internal/model", "internal/httpclient",
			"internal/i18n", "internal/version",
		}},
	{2, "capabilities: they implement the contract and know nothing of the loop",
		[]string{"internal/tool", "internal/provider", "internal/ext"}},
	{3, "the turn loop and the context maintenance it drives",
		[]string{"internal/agent", "internal/compaction"}},
	{4, "wiring: the one place that names a concrete implementation",
		[]string{"internal/assembly"}},
	{5, "frontends: they drive the controller, and nothing drives them",
		[]string{"internal/frontend"}},
	{6, "the process entry point, which only calls the CLI layer",
		[]string{"cmd"}},
}

// isolatedLayers import nothing from outside themselves. The boundary is the layer rather than each
// subpackage: the event vocabulary carries provider.Message and provider.Usage, and one layer with one
// vocabulary is what §6.2 means by a contract layer. An edge *out* of the layer would put a foundation
// above what it founds, which is still forbidden; edges *within* it are not a layering breach, and
// acyclicity is checked separately so they cannot smuggle in a loop.
var isolatedLayers = []string{"internal/contract"}

// exclusive groups may not import each other in either direction: the capability
// packages share the contract as their vocabulary, not each other's internals.
var exclusive = [][]string{
	{"internal/tool", "internal/provider", "internal/ext"},
}

// namedEdges are the absolutes §6.2 states directly, which direction alone would
// allow because the target sits lower in the graph.
var namedEdges = []struct {
	from string
	to   string
	why  string
}{
	{"internal/frontend/termrender", "internal/agent",
		"the renderer draws values; it must not know the loop exists"},
	{"internal/frontend/termrender", "internal/session",
		"the renderer holds no session state"},
	{"internal/perm", "internal/agent",
		"the agent consults a Gate; policy never reaches up into the loop"},
	{"internal/sandbox", "internal/agent",
		"the sandbox is data the agent supplies, not a caller of it"},
}

// rankOf returns the declared rank of a package path and whether one was
// declared at all. The longest matching prefix wins, so internal/contract/tool
// resolves to internal/contract rather than to internal/tool.
func rankOf(pkg string) (int, bool) {
	best := -1
	rank := 0
	for _, layer := range layers {
		for _, base := range layer.paths {
			if under(pkg, base) && len(base) > best {
				rank, best = layer.rank, len(base)
			}
		}
	}
	return rank, best >= 0
}

// under reports whether pkg is base or a subpackage of it, comparing whole path
// elements so that internal/perm never matches internal/permission.
func under(pkg, base string) bool {
	return pkg == base || strings.HasPrefix(pkg, base+"/")
}

// breach reports why one package importing another is illegal, or "" when the
// edge is fine. Every rule lives in this one function so the whole policy is
// readable in a single screen.
func breach(from, to string) string {
	fromRank, fromRanked := rankOf(from)
	toRank, toRanked := rankOf(to)
	if !fromRanked || !toRanked {
		// Reported by TestEveryPackageIsRanked; judging it here would produce a
		// second, less accurate complaint about the same problem.
		return ""
	}

	for _, layer := range isolatedLayers {
		if under(from, layer) && !under(to, layer) {
			return "this layer imports nothing from outside " + layer
		}
	}

	for _, group := range exclusive {
		mine := groupMember(from, group)
		if mine == "" {
			continue
		}
		for _, other := range group {
			if other != mine && under(to, other) {
				return "capability packages never import each other; the contract is their shared vocabulary"
			}
		}
	}

	for _, rule := range namedEdges {
		if under(from, rule.from) && under(to, rule.to) {
			return rule.why
		}
	}

	if toRank > fromRank {
		return fmt.Sprintf("an upward import: rank %d importing rank %d", fromRank, toRank)
	}
	return ""
}

// groupMember returns the member of group that pkg belongs to, or "".
func groupMember(pkg string, group []string) string {
	for _, member := range group {
		if under(pkg, member) {
			return member
		}
	}
	return ""
}

// edge is one import of another package in this module.
type edge struct {
	from, to string // package paths, relative to the module root
	file     string // absolute, so a failure can name the line
	line     int
}

// graph is what one scan of the module found.
type graph struct {
	root     string
	packages []string
	edges    []edge
}

// scan parses the imports of every Go file in the module. It reads import
// declarations only — no type information — so it needs nothing beyond the
// standard library (docs/GO-STANDARDS.md §7 keeps the dependency list short).
func scan(t *testing.T) graph {
	t.Helper()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	found := map[string]bool{}
	var edges []edge

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || skipped[entry.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		from := packagePath(root, path)
		found[from] = true
		for _, spec := range file.Imports {
			imported, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if !strings.HasPrefix(imported, modulePath+"/") {
				continue
			}
			to := strings.TrimPrefix(imported, modulePath+"/")
			// A package's own external test package — `package x_test`, importing the
			// package beside it — is not a module dependency. The graph is keyed on
			// directories, so that import looks exactly like a self-edge, and recording
			// it as one fails the contract layer's "imports nothing else in the module"
			// rule and the acyclicity check for the most ordinary arrangement in Go.
			// The edge is dropped here, where the misreading happens, rather than
			// exempted in each rule that would otherwise trip over it.
			if to == from {
				continue
			}
			edges = append(edges, edge{
				from: from,
				to:   to,
				file: path,
				line: fset.Position(spec.Pos()).Line,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}

	packages := make([]string, 0, len(found))
	for pkg := range found {
		packages = append(packages, pkg)
	}
	sort.Strings(packages)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})
	return graph{root: root, packages: packages, edges: edges}
}

// skipped are directories that never hold module source.
var skipped = map[string]bool{
	"bin": true, "dist": true, "testdata": true, "vendor": true, "node_modules": true,
}

// packagePath turns a file path into its package path relative to the module
// root, using slashes so the result matches import paths on every platform.
func packagePath(root, file string) string {
	rel, err := filepath.Rel(root, filepath.Dir(file))
	if err != nil {
		return filepath.ToSlash(filepath.Dir(file))
	}
	if rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}

// moduleRoot walks up from the test's directory to the directory holding go.mod,
// so the test does not depend on being run from the module root.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getting the working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

// TestScanFindsTheModule guards the suite against passing because it looked at
// nothing. A scan that silently found no packages — a wrong root, a skipped
// directory — would make every other test here vacuously true, which is the
// failure mode that makes an architecture test worse than none: it reports
// compliance it never checked.
func TestScanFindsTheModule(t *testing.T) {
	t.Parallel()
	g := scan(t)
	for _, want := range []string{
		"internal/agent",
		"internal/architecture",
		"internal/contract/tool",
		"internal/frontend/tui",
		"internal/tool/builtin",
	} {
		if !slices.Contains(g.packages, want) {
			t.Fatalf("the scan did not find %s; it found %d packages: %v", want, len(g.packages), g.packages)
		}
	}
	// The entry point is asserted by prefix rather than by name: naming it would
	// make this package a second owner of the binary name (E1-T8).
	if !slices.ContainsFunc(g.packages, func(pkg string) bool { return strings.HasPrefix(pkg, "cmd/") }) {
		t.Fatalf("no package under cmd/ was found, so the scan is missing the entry point: %v", g.packages)
	}
	if len(g.edges) == 0 {
		t.Fatal("the scan found no internal imports at all, but internal/frontend/cli imports internal/version, so it cannot be right")
	}
}

func TestEveryPackageIsRanked(t *testing.T) {
	t.Parallel()
	g := scan(t)
	var unranked []string
	for _, pkg := range g.packages {
		if _, ok := rankOf(pkg); !ok {
			unranked = append(unranked, pkg)
		}
	}
	if len(unranked) > 0 {
		t.Fatalf("packages with no declared layer:\n\t%s\n\n"+
			"Add each to `layers` in this file. An unranked package is not covered by the rule, "+
			"which is worse than an obvious violation because nothing fails.",
			strings.Join(unranked, "\n\t"))
	}
}

func TestImportsRespectTheLayering(t *testing.T) {
	t.Parallel()
	g := scan(t)
	violations := make([]string, 0, len(g.edges))
	for _, e := range g.edges {
		why := breach(e.from, e.to)
		if why == "" {
			continue
		}
		rel, err := filepath.Rel(g.root, e.file)
		if err != nil {
			rel = e.file
		}
		violations = append(violations, fmt.Sprintf("%s:%d  %s imports %s — %s",
			rel, e.line, e.from, e.to, why))
	}
	if len(violations) > 0 {
		t.Fatalf("the import graph breaks the layering in docs/TECHNICAL_DESIGN.md §6.2:\n\t%s",
			strings.Join(violations, "\n\t"))
	}
}

func TestModuleImportGraphIsAcyclic(t *testing.T) {
	t.Parallel()
	g := scan(t)

	adjacency := make(map[string][]string, len(g.edges))
	for _, e := range g.edges {
		// No self-edge can reach here: scan drops them, because a package's own
		// external test package is not a module dependency and the graph is keyed on
		// directories. The comment there is the one place that is explained.
		adjacency[e.from] = append(adjacency[e.from], e.to)
	}

	const (
		unvisited = iota
		onStack
		settled
	)
	state := make(map[string]int, len(g.packages))
	stack := make([]string, 0, len(g.packages))
	var cycle []string

	var visit func(node string) bool
	visit = func(node string) bool {
		state[node] = onStack
		stack = append(stack, node)
		for _, next := range adjacency[node] {
			switch state[next] {
			case onStack:
				cycle = append(append(make([]string, 0, len(stack)+1), stack...), next)
				return true
			case unvisited:
				if visit(next) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = settled
		return false
	}

	for _, node := range g.packages {
		if state[node] == unvisited && visit(node) {
			t.Fatalf("import cycle: %s", strings.Join(cycle, " -> "))
		}
	}
}

// TestBreachRecognisesTheDocumentedViolations is what keeps the rule engine
// honest: it asserts the two examples §6.2 names, plus the rest of the policy.
// Without it, a rule that silently allowed everything would still pass the scan
// tests on a conforming tree.
func TestBreachRecognisesTheDocumentedViolations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		from       string
		to         string
		wantBreach bool
	}{
		{"the contract layer importing out of itself", "internal/contract/tool", "internal/session", true},
		// Allowed deliberately since E4-T1: the event vocabulary carries provider.Message, so the
		// isolation boundary is the layer rather than each subpackage. Pinned here so the relaxation is
		// visible rather than an accident of a later edit.
		{"a contract subpackage importing its own sibling", "internal/contract/event", "internal/contract/provider", false},
		{"the renderer importing the agent", "internal/frontend/termrender", "internal/agent", true},
		{"the renderer importing session state", "internal/frontend/termrender", "internal/session", true},
		{"policy importing the loop", "internal/perm", "internal/agent", true},
		{"the sandbox importing the loop", "internal/sandbox", "internal/agent", true},
		{"a capability importing a sibling capability", "internal/ext/mcp", "internal/tool", true},
		{"an upward import", "internal/session", "internal/agent", true},
		{"a foundation import skipping two ranks", "internal/config", "internal/frontend/tui", true},
		{"wiring the concrete kinds", "internal/assembly", "internal/provider/openai", false},
		{"a frontend importing the contract", "internal/frontend/tui", "internal/contract/tool", false},
		{"the cli importing version", "internal/frontend/cli", "internal/version", false},
		{"a package under cmd importing the cli", "cmd/entrypoint", "internal/frontend/cli", false},
		{"the loop importing a capability", "internal/agent", "internal/tool", false},
		{"the loop driving compaction", "internal/agent", "internal/compaction", false},
		{"a frontend importing its own sibling", "internal/frontend/tui", "internal/frontend/termrender", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := breach(tc.from, tc.to)
			switch {
			case tc.wantBreach && got == "":
				t.Fatalf("%s -> %s should be a violation, but the rule allowed it", tc.from, tc.to)
			case !tc.wantBreach && got != "":
				t.Fatalf("%s -> %s should be allowed, but the rule said: %s", tc.from, tc.to, got)
			}
		})
	}
}
