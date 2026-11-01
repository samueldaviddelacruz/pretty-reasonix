package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"pretty-reasonix/internal/contract/tool"
)

// Ceilings on what a search may cost.
//
// Named rather than inlined because they are budgets a person can argue with. A result ceiling exists so a
// broad `glob` cannot fill the context with paths; a timeout exists because a search over a checkout that
// turns out to be enormous should end with a partial answer rather than a hung turn.
const (
	// MaxGrepMatches is how many lines a grep returns before it stops.
	MaxGrepMatches = 200
	// MaxGlobMatches is how many paths a glob returns before it stops.
	MaxGlobMatches = 500
	// SearchTimeout bounds one glob or grep.
	SearchTimeout = 30 * time.Second
	// DefaultReadLines is how much of a file read_file returns when no limit is given.
	DefaultReadLines = 2000
)

// noise is what a listing and a search skip.
//
// Version control and dependency directories are noise for every question a model asks: they are large, they
// are not the user's code, and a search that returns them is a search the model has to filter. Naming them
// once here means a new tool cannot forget one of them and be inconsistent with its neighbours.
var noise = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".jj": true,
	"node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".mypy_cache": true, ".pytest_cache": true,
	"target": true, "dist": true, "build": true,
}

// ReadFile is the tool that reads a file.
type ReadFile struct{ root *os.Root }

// Ls is the tool that lists a directory.
type Ls struct{ root *os.Root }

// Glob is the tool that finds paths by pattern.
type Glob struct{ root *os.Root }

// Grep is the tool that searches file contents.
type Grep struct{ root *os.Root }

// NewReadFile makes the tool, confined to a workspace root.
func NewReadFile(root *os.Root) ReadFile { return ReadFile{root: root} }

// NewLs makes the tool, confined to a workspace root.
func NewLs(root *os.Root) Ls { return Ls{root: root} }

// NewGlob makes the tool, confined to a workspace root.
func NewGlob(root *os.Root) Glob { return Glob{root: root} }

// NewGrep makes the tool, confined to a workspace root.
func NewGrep(root *os.Root) Grep { return Grep{root: root} }

// Name is "read_file".
func (ReadFile) Name() string { return "read_file" }

// Description is what the model reads to decide whether to call it.
func (ReadFile) Description() string {
	return "Read a text file. Returns numbered lines so you can cite them or ask for a range."
}

// Schema is the arguments read_file takes.
func (ReadFile) Schema() json.RawMessage { return readFileSchema }

// ReadOnly is true: reading changes nothing, so it may run beside another read.
func (ReadFile) ReadOnly() bool { return true }

// Execute reads a file.
func (r ReadFile) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{}, fmt.Errorf("read_file arguments: %w", err)
	}
	if in.Limit <= 0 {
		in.Limit = DefaultReadLines
	}
	file, err := r.root.Open(in.Path)
	if err != nil {
		// A file that is not there is a fact about the world, not a broken tool: the model is told and can
		// try another path.
		return tool.Result{Err: fmt.Errorf("%s: %w", in.Path, err), Truncated: false}, nil
	}
	defer func() { _ = file.Close() }()

	var out strings.Builder
	scanner := bufio.NewScanner(file)
	// Long lines happen — a minified file, a JSON blob — and the default 64 KiB token limit would make
	// read_file fail on a file it should partially return.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	line := 0
	written := 0
	truncated := false
	for scanner.Scan() {
		line++
		if line <= in.Offset {
			continue
		}
		if written >= in.Limit {
			truncated = true
			break
		}
		// Numbered because the model then has something to cite and something to ask a follow-up range from;
		// unnumbered output forces it to count.
		fmt.Fprintf(&out, "%6d→%s\n", line, scanner.Text())
		written++
	}
	if err := scanner.Err(); err != nil {
		return tool.Result{Err: fmt.Errorf("%s: %w", in.Path, err)}, nil
	}
	if written == 0 && line == 0 {
		return tool.Result{Output: "(the file is empty)"}, nil
	}
	if written == 0 {
		return tool.Result{Output: fmt.Sprintf("(nothing at offset %d; the file has %d lines)", in.Offset, line)}, nil
	}
	return tool.Result{Output: out.String(), Truncated: truncated}, nil
}

// Name is "ls".
func (Ls) Name() string { return "ls" }

// Description is what the model reads.
func (Ls) Description() string {
	return "List a directory. Skips version control and dependency directories, and marks directories with a trailing slash."
}

// Schema is the arguments ls takes.
func (Ls) Schema() json.RawMessage { return lsSchema }

// ReadOnly is true.
func (Ls) ReadOnly() bool { return true }

// Execute lists a directory.
func (l Ls) Execute(_ context.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{}, fmt.Errorf("ls arguments: %w", err)
	}
	if in.Path == "" {
		in.Path = "."
	}
	dir, err := l.root.Open(in.Path)
	if err != nil {
		return tool.Result{Err: fmt.Errorf("%s: %w", in.Path, err)}, nil
	}
	defer func() { _ = dir.Close() }()

	entries, err := dir.ReadDir(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		return tool.Result{Err: fmt.Errorf("%s: %w", in.Path, err)}, nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if noise[entry.Name()] {
			continue
		}
		if entry.IsDir() {
			names = append(names, entry.Name()+"/")
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return tool.Result{Output: "(nothing here)"}, nil
	}
	sort.Strings(names)
	return tool.Result{Output: strings.Join(names, "\n")}, nil
}

// Name is "glob".
func (Glob) Name() string { return "glob" }

// Description is what the model reads.
func (Glob) Description() string {
	return "Find files by glob pattern. ** matches any number of directories. Results are capped."
}

// Schema is the arguments glob takes.
func (Glob) Schema() json.RawMessage { return globSchema }

// ReadOnly is true.
func (Glob) ReadOnly() bool { return true }

// Execute finds paths by pattern.
func (g Glob) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{}, fmt.Errorf("glob arguments: %w", err)
	}
	if in.Pattern == "" {
		return tool.Result{Err: errors.New("glob needs a pattern")}, nil
	}
	matcher, err := compileGlob(in.Pattern)
	if err != nil {
		return tool.Result{Err: err}, nil
	}

	searchCtx, cancel := context.WithTimeout(ctx, SearchTimeout)
	defer cancel()

	var matches []string
	truncated := false
	walkErr := walkTree(searchCtx, g.root, func(rel string, entry fs.DirEntry) bool {
		if entry.IsDir() {
			return true
		}
		if !matcher.MatchString(rel) {
			return true
		}
		if len(matches) >= MaxGlobMatches {
			truncated = true
			return false
		}
		matches = append(matches, rel)
		return true
	})

	sort.Strings(matches)
	switch {
	case walkErr != nil:
		return tool.Result{Err: walkErr, Truncated: truncated}, nil
	case len(matches) == 0:
		return tool.Result{Output: "(no paths match)"}, nil
	}
	return tool.Result{Output: strings.Join(matches, "\n"), Truncated: truncated}, nil
}

// Name is "grep".
func (Grep) Name() string { return "grep" }

// Description is what the model reads.
func (Grep) Description() string {
	return "Search file contents by regular expression. Optionally restrict by path and by a file glob. Results and time are capped."
}

// Schema is the arguments grep takes.
func (Grep) Schema() json.RawMessage { return grepSchema }

// ReadOnly is true.
func (Grep) ReadOnly() bool { return true }

// Execute searches file contents.
func (g Grep) Execute(ctx context.Context, args json.RawMessage) (tool.Result, error) {
	var in struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return tool.Result{}, fmt.Errorf("grep arguments: %w", err)
	}
	expression, err := regexp.Compile(in.Pattern)
	if err != nil {
		return tool.Result{Err: fmt.Errorf("the pattern is not a regular expression: %w", err)}, nil
	}
	var only *regexp.Regexp
	if in.Glob != "" {
		if only, err = compileGlob(in.Glob); err != nil {
			return tool.Result{Err: err}, nil
		}
	}
	start := in.Path
	if start == "" {
		start = "."
	}

	searchCtx, cancel := context.WithTimeout(ctx, SearchTimeout)
	defer cancel()

	var found []string
	truncated := false
	stop := false
	walkErr := walkTree(searchCtx, g.root, func(rel string, entry fs.DirEntry) bool {
		if stop || entry.IsDir() || !under(rel, start) || (only != nil && !only.MatchString(rel)) {
			return !stop
		}
		lines, more := grepFile(g.root, rel, expression, MaxGrepMatches-len(found))
		found = append(found, lines...)
		if more || len(found) >= MaxGrepMatches {
			truncated = true
			stop = true
		}
		return !stop
	})

	sort.Strings(found)
	switch {
	case walkErr != nil:
		return tool.Result{Err: walkErr, Truncated: truncated}, nil
	case len(found) == 0:
		return tool.Result{Output: "(no matches)"}, nil
	}
	return tool.Result{Output: strings.Join(found, "\n"), Truncated: truncated}, nil
}

// grepFile searches one file, returning "path:line: text" for each hit up to budget.
func grepFile(root *os.Root, rel string, expression *regexp.Regexp, budget int) ([]string, bool) {
	file, err := root.Open(rel)
	if err != nil {
		return nil, false
	}
	defer func() { _ = file.Close() }()

	// A binary file is skipped rather than searched: its "lines" are not text, and a match inside one is
	// noise the model cannot use.
	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return nil, false
	}
	if bytes0(head[:n]) {
		return nil, false
	}

	var hits []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if !expression.MatchString(scanner.Text()) {
			continue
		}
		if len(hits) >= budget {
			return hits, true
		}
		hits = append(hits, fmt.Sprintf("%s:%d: %s", rel, line, strings.TrimSpace(scanner.Text())))
	}
	return hits, false
}

// walkTree visits every entry under the root, in lexical order, skipping noise.
//
// One walk for both search tools: they differ in what they keep, not in how they get there, and two copies of
// this would drift on the day somebody adds a directory to the skip list.
func walkTree(ctx context.Context, root *os.Root, visit func(rel string, entry fs.DirEntry) bool) error {
	return fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is skipped rather than fatal: a search that returns everything
			// it could reach beats one that returns nothing.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() && rel != "." && noise[entry.Name()] {
			return fs.SkipDir
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		if !visit(rel, entry) {
			return fs.SkipAll
		}
		return nil
	})
}

// compileGlob turns a glob into an expression, supporting ** for any number of directories.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(".*")
			i++
		case pattern[i] == '*':
			b.WriteString("[^/]*")
		case pattern[i] == '?':
			b.WriteString("[^/]")
		case strings.ContainsRune(`.+()|[]{}^$\`, rune(pattern[i])):
			b.WriteString(`\` + string(pattern[i]))
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	compiled, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("the glob %q is not valid: %w", pattern, err)
	}
	return compiled, nil
}

// under reports whether a path is inside a directory, treating "." as everything.
func under(rel, dir string) bool {
	dir = strings.TrimPrefix(path.Clean(dir), "./")
	return dir == "" || dir == "." || rel == dir || strings.HasPrefix(rel, dir+"/")
}

// bytes0 reports whether a sample looks binary, which is a NUL in the first block.
func bytes0(sample []byte) bool { return slices.Contains(sample, 0) }

var (
	readFileSchema = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path, relative to the workspace"},"offset":{"type":"integer","description":"Lines to skip before returning any"},"limit":{"type":"integer","description":"Maximum lines to return; defaults to 2000"}},"required":["path"]}`)
	lsSchema       = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path, relative to the workspace; defaults to the root"}}}`)
	globSchema     = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Glob pattern; ** matches any number of directories"}},"required":["pattern"]}`)
	grepSchema     = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regular expression to search for"},"path":{"type":"string","description":"Directory to search under; defaults to the workspace root"},"glob":{"type":"string","description":"Only search files whose path matches this glob"}},"required":["pattern"]}`)
)
