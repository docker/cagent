package main

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/prog"
	"golang.org/x/tools/go/packages"
)

var frozenConfigPath = regexp.MustCompile(`(^|/)pkg/config/v\d+/`)

// Shipped config versions keep their original wire format and implementation.
func outsideFrozenConfig(p *cop.Pass) bool {
	return !frozenConfigPath.MatchString(filepath.ToSlash(p.Filename()))
}

func sharedFileCop(newCop func(...cop.FuncOption) *prog.Func, opts ...cop.FuncOption) *cop.Func {
	var fileCop *cop.Func
	newCop(append(opts, func(c *cop.Func) { fileCop = c })...)
	return fileCop
}

// The file runner visits inactive-only directories that go/packages omits.
func withDirectoryTypes(c *cop.Func) *cop.Func {
	type checkedPackage struct {
		files map[string]*ast.File
		info  *types.Info
		pkg   *types.Package
	}
	var mu sync.Mutex
	var fset *token.FileSet
	var cache map[string]checkedPackage
	var imports types.Importer
	wrapped := *c
	wrapped.Types = false
	wrapped.Run = func(p *cop.Pass) {
		mu.Lock()
		defer mu.Unlock()
		if fset != p.FileSet {
			fset = p.FileSet
			cache = make(map[string]checkedPackage)
			imports = importer.Default()
		}
		name, err := filepath.Abs(p.Filename())
		if err != nil {
			p.Reportf(p.File, "cannot resolve source path: %v", err)
			return
		}
		dir := filepath.Dir(name)
		key := dir + "/" + p.File.Name.Name
		checked, ok := cache[key]
		if !ok {
			entries, err := os.ReadDir(dir)
			if err != nil {
				p.Reportf(p.File, "cannot read package: %v", err)
				return
			}
			parsed := make(map[string]*ast.File)
			var files []*ast.File
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}
				path := filepath.Join(dir, entry.Name())
				file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if err != nil || file.Name.Name != p.File.Name.Name {
					continue
				}
				parsed[path] = file
				files = append(files, file)
			}
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue),
				Defs:  make(map[*ast.Ident]types.Object),
				Uses:  make(map[*ast.Ident]types.Object),
			}
			// Platform variants may conflict; retain whichever calls resolve.
			cfg := types.Config{Importer: imports, Error: func(error) {}}
			typed, _ := cfg.Check(dir, fset, files, info)
			checked = checkedPackage{files: parsed, info: info, pkg: typed}
			cache[key] = checked
		}
		file := checked.files[name]
		if file == nil {
			return
		}
		pass := &cop.Pass{Cop: c, FileSet: fset, File: file, Info: checked.info, Package: checked.pkg}
		c.Check(pass)
		tokens := fset.File(file.Pos())
		for _, offense := range pass.Offenses() {
			p.ReportAt(tokens.Pos(offense.Pos.Offset), tokens.Pos(offense.End.Offset), offense.Message)
		}
	}
	return &wrapped
}

// A loaded program is one source snapshot; never reuse types across runs.
type typedPackageLoader struct {
	mu       sync.Mutex
	program  *prog.Program
	packages []*packages.Package
	err      error
}

func (l *typedPackageLoader) load(p *prog.Pass) ([]*packages.Package, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.program == p.Program {
		return l.packages, l.err
	}
	l.program = p.Program
	l.packages, l.err = loadTypedPackages(p)
	return l.packages, l.err
}

func loadTypedPackages(p *prog.Pass) ([]*packages.Package, error) {
	var dirs []string
	for _, pkg := range p.Program.Packages {
		if pkg.Dir != "" {
			dirs = append(dirs, pkg.Dir)
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	return packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedModule,
		Dir: dirs[0], Fset: p.Program.Fset, Tests: true,
	}, dirs...)
}

// Reuse shared matching and package loading, retaining each cop's type-error policy.
func withTypedFiles(load func(*prog.Pass) ([]*packages.Package, error), newCop func(...cop.FuncOption) *prog.Func, skipIllTyped bool, opts ...cop.FuncOption) *prog.Func {
	fileCop := sharedFileCop(newCop, opts...)
	return prog.New(fileCop.Meta, func(p *prog.Pass) {
		pkgs, err := load(p)
		if err != nil {
			message := "cannot inspect packages: %v"
			if skipIllTyped {
				message = "cannot inspect modernization candidates: %v"
			}
			p.Reportf(token.NoPos, message, err)
			return
		}
		seen := make(map[string]bool)
		for _, pkg := range pkgs {
			if skipIllTyped && pkg.IllTyped {
				continue
			}
			for _, file := range pkg.Syntax {
				pass := &cop.Pass{Cop: fileCop, FileSet: p.Program.Fset, File: file, Info: pkg.TypesInfo, Package: pkg.Types}
				if seen[pass.Filename()] || !fileCop.InScope(pass) {
					continue
				}
				seen[pass.Filename()] = true
				fileCop.Check(pass)
				for _, offense := range pass.Offenses() {
					p.ReportOffense(offense)
				}
			}
		}
	})
}
