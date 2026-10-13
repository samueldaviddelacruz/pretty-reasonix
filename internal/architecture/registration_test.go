package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// providerPackage is the contract package whose Register has exactly one home.
	providerPackage = "pretty-reasonix/internal/contract/provider"
	// kindPackagePrefix is where the concrete kinds live.
	kindPackagePrefix = "pretty-reasonix/internal/provider/"
	// assemblyPrefix is the one package allowed to register them.
	assemblyPrefix = "internal/assembly/"
)

// registrationCalls returns the line of every call to provider.Register in one file.
//
// The local name is resolved from the file's own imports, so an aliased import is
// caught rather than missed, and the match is on the syntax tree rather than on text,
// which cuts both ways on purpose: a mention in a comment or a string is not a call,
// and a real call cannot hide behind an unusual spelling.
func registrationCalls(path string, body []byte) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, body, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	local := make(map[string]bool)
	for _, spec := range file.Imports {
		imported, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			return nil, unquoteErr
		}
		if imported != providerPackage {
			continue
		}
		// The package's own name when the import is not aliased, and the alias when it
		// is. Without this, `import p "…/provider"; p.Register(…)` would pass.
		name := "provider"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		local[name] = true
	}
	if len(local) == 0 {
		return nil, nil
	}

	var lines []int
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Register" {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || !local[receiver.Name] {
			return true
		}
		lines = append(lines, fset.Position(call.Pos()).Line)
		return true
	})
	return lines, nil
}

// blankImportsOfKinds returns the lines where a file blank-imports a concrete kind.
//
// `_ "something"` is how a package arranges for another package's init() to run without
// naming it, which is the self-registration this design does not use: a kind registered
// that way would make the set of available providers depend on which packages a build
// happened to link, and nothing at the call site would say so (rule [#3]).
func blankImportsOfKinds(path string, body []byte) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, body, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var lines []int
	for _, spec := range file.Imports {
		if spec.Name == nil || spec.Name.Name != "_" {
			continue
		}
		imported, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			return nil, unquoteErr
		}
		if strings.HasPrefix(imported, kindPackagePrefix) {
			lines = append(lines, fset.Position(spec.Pos()).Line)
		}
	}
	return lines, nil
}

// scanRegistration reports every registration call outside the assembly, and every
// blank import of a concrete kind anywhere.
func scanRegistration(t *testing.T, root string) ([]string, []string) {
	t.Helper()
	var outsideAssembly, blankImports []string
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippingDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// A test is not the program's wiring. Rule [#3] is about where the product decides
		// which kinds it can build — the set that should be visible in one file — and a
		// test that registers a stub kind to exercise the registry is not that. The
		// exemption is also forced rather than chosen: the test for Register has to call
		// Register, so the rule would be unsatisfiable without it.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		lines, scanErr := registrationCalls(rel, body)
		if scanErr != nil {
			return scanErr
		}
		for _, line := range lines {
			if !strings.HasPrefix(rel, assemblyPrefix) {
				outsideAssembly = append(outsideAssembly, fmt.Sprintf("%s:%d", rel, line))
			}
		}
		blank, blankErr := blankImportsOfKinds(rel, body)
		if blankErr != nil {
			return blankErr
		}
		for _, line := range blank {
			blankImports = append(blankImports, fmt.Sprintf("%s:%d", rel, line))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("scanning %s: %v", root, walkErr)
	}
	if scanned < 20 {
		t.Fatalf("the scan only read %d Go files, so it is not walking the repository", scanned)
	}
	return outsideAssembly, blankImports
}

// TestProviderKindsAreRegisteredOnlyFromTheAssembly is rule [#3] and TDD §6.2: the set
// of available providers is meant to be visible in one file rather than implied by which
// packages a build happened to link.
func TestProviderKindsAreRegisteredOnlyFromTheAssembly(t *testing.T) {
	t.Parallel()
	outsideAssembly, _ := scanRegistration(t, moduleRoot(t))
	if len(outsideAssembly) > 0 {
		t.Fatalf("provider kinds are registered only from %s (rule [#3], TDD §6.2); these call provider.Register:\n\t%s",
			assemblyPrefix, strings.Join(outsideAssembly, "\n\t"))
	}
}

// TestNoPackageBlankImportsAProviderKind closes the other door to the same place: a blank
// import makes a kind's init() run without naming it, which reaches the same invisible
// coupling by a route the call-site check above cannot see.
func TestNoPackageBlankImportsAProviderKind(t *testing.T) {
	t.Parallel()
	_, blankImports := scanRegistration(t, moduleRoot(t))
	if len(blankImports) > 0 {
		t.Fatalf("a concrete kind may only be imported by the assembly, and by name; these blank-import one:\n\t%s",
			strings.Join(blankImports, "\n\t"))
	}
}

// TestTheRegistrationScanCatchesWhatItLooksFor is the control, and today it is
// load-bearing rather than decorative: nothing calls provider.Register yet, because
// internal/assembly is still a stub, so the two tests above pass on an empty set. Without
// this, the scanner could stop seeing anything and every ticket would still be green —
// until the first kind arrived and registered itself from the wrong place.
func TestTheRegistrationScanCatchesWhatItLooksFor(t *testing.T) {
	t.Parallel()

	const plainCall = `package x

import "pretty-reasonix/internal/contract/provider"

func boot() error {
	return provider.Register("test", nil)
}
`
	lines, err := registrationCalls("internal/somewhere/boot.go", []byte(plainCall))
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("found %d calls in a fixture with exactly one; the scan is not seeing what it should", len(lines))
	}

	const aliasedCall = `package x

import p "pretty-reasonix/internal/contract/provider"

func boot() error {
	return p.Register("test", nil)
}
`
	if aliased, err := registrationCalls("internal/somewhere/boot.go", []byte(aliasedCall)); err != nil {
		t.Fatalf("scanning the aliased fixture: %v", err)
	} else if len(aliased) != 1 {
		t.Error("an aliased import was missed, so the rule can be side-stepped by renaming the import")
	}

	// The other direction: a false positive here would fail a build over prose.
	const prose = `package x

// provider.Register is called from the assembly, and nowhere else.
const note = "provider.Register is not called here"
`
	if found, err := registrationCalls("internal/somewhere/prose.go", []byte(prose)); err != nil {
		t.Fatalf("scanning the prose fixture: %v", err)
	} else if len(found) != 0 {
		t.Errorf("prose was read as a call: %v", found)
	}

	const blankImport = `package x

import _ "pretty-reasonix/internal/provider/openai"
`
	if found, err := blankImportsOfKinds("internal/somewhere/kinds.go", []byte(blankImport)); err != nil {
		t.Fatalf("scanning the blank-import fixture: %v", err)
	} else if len(found) != 1 {
		t.Errorf("a blank import of a concrete kind was missed: %v", found)
	}

	// A named import of a kind is what the assembly itself does, so it must not be read
	// as the same thing.
	const namedImport = `package x

import "pretty-reasonix/internal/provider/openai"
`
	if found, err := blankImportsOfKinds("internal/assembly/boot.go", []byte(namedImport)); err != nil {
		t.Fatalf("scanning the named-import fixture: %v", err)
	} else if len(found) != 0 {
		t.Errorf("a named import of a kind was read as a blank one: %v", found)
	}
}
