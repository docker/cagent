package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	rubocops "github.com/dgageot/rubocop-go/cops"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

func TestTypedPackageLoader(t *testing.T) {
	t.Parallel()
	var loader typedPackageLoader
	first := typedPackageFixture(t)
	pass := &prog.Pass{Program: first}
	pkgs, err := loader.load(pass)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs)
	assert.True(t, slices.ContainsFunc(pkgs, func(pkg *packages.Package) bool {
		return slices.ContainsFunc(pkg.GoFiles, func(name string) bool { return filepath.Base(name) == "sample_test.go" })
	}))

	cached, err := loader.load(pass)
	require.NoError(t, err)
	assert.Same(t, pkgs[0], cached[0])

	dir := first.Packages[0].Dir
	require.NoError(t, os.WriteFile(filepath.Join(dir, "added.go"), []byte("package p\nfunc added() {}\n"), 0o600))
	second, err := prog.LoadDir(dir, "./...")
	require.NoError(t, err)
	fresh, err := loader.load(&prog.Pass{Program: second})
	require.NoError(t, err)
	require.NotEmpty(t, fresh)
	assert.NotSame(t, pkgs[0], fresh[0])
	assert.NotSame(t, first.Fset, second.Fset)
	assert.NotNil(t, fresh[0].Types.Scope().Lookup("added"))
}

func TestSharedTypedCopsMatchUpstream(t *testing.T) {
	t.Parallel()
	constructors := []func(...cop.FuncOption) *prog.Func{
		rubocops.NewLintErrorsAsType,
		rubocops.NewLintMapsCopy,
		rubocops.NewLintMapsClone,
		rubocops.NewLintSlicesContains,
		rubocops.NewLintSlicesEqual,
		rubocops.NewLintSplitSeq,
		rubocops.NewLintSortedMapKeys,
		rubocops.NewLintWaitGroupGo,
		rubocops.NewLintHTTPTestRequestWithContext,
	}
	for _, brokenTests := range []bool{false, true} {
		files := coptest.ProgramFiles{
			"go.mod": "module example.test\n\ngo 1.27\n",
			"sample.go": `package p
import ("errors"; "strings"; "sync"; "sort"; "net/http/httptest")
func copyMap(dst, src map[string]int) { for k, v := range src { dst[k] = v } }
func cloneMap(src map[string]int) map[string]int { if src == nil { return nil }; dst := make(map[string]int, len(src)); for k, v := range src { dst[k] = v }; return dst }
func contains(xs []int, x int) bool { for _, v := range xs { if v == x { return true } }; return false }
func equal(xs, ys []int) bool { if len(xs) != len(ys) { return false }; for i := range xs { if xs[i] != ys[i] { return false } }; return true }
func split(s string) { for _, v := range strings.Split(s, ",") { println(v) } }
func keys(m map[string]int) []string { var keys []string; for k := range m { keys = append(keys, k) }; sort.Strings(keys); return keys }
func work() { var wg sync.WaitGroup; wg.Add(1); go func() { defer wg.Done() }(); wg.Wait() }
func as(err *httpError) bool { var target *httpError; if errors.As(err, &target) { return target != nil }; return false }
type httpError struct{}
func (*httpError) Error() string { return "error" }
var _ = httptest.NewRequest
`,
			"sample_test.go": `package p
import ("testing"; "context"; "net/http/httptest"; "strings")
func TestRequest(t *testing.T) { _ = httptest.NewRequest("GET", "/", nil).WithContext(context.Background()) }
func words(s string) { for _, v := range strings.Split(s, ",") { println(v) } }
`,
			"external_test.go":        "package p_test\nimport \"strings\"\nfunc words(s string) { for _, v := range strings.Split(s, \",\") { println(v) } }\n",
			"testonly/sample_test.go": "package p\nimport \"strings\"\nfunc words(s string) { for _, v := range strings.Split(s, \",\") { println(v) } }\n",
		}
		if brokenTests {
			files["sample_test.go"] += "\nvar _ = missing\n"
		}
		check := prog.New(cop.Meta{Name: "test"}, func(p *prog.Pass) {
			for _, newCop := range constructors {
				upstream := newCop(cop.WithScope(outsideFrozenConfig))
				shared := registeredSharedCop(t, programCops, upstream.Name())
				want := &prog.Pass{Cop: upstream, Program: p.Program}
				got := &prog.Pass{Cop: shared, Program: p.Program}
				upstream.Check(want)
				shared.Check(got)
				if !brokenTests {
					assert.NotEmpty(t, want.Offenses(), upstream.Name())
				}
				assert.ElementsMatch(t, want.Offenses(), got.Offenses(), upstream.Name())
			}
		})
		coptest.RunProgram(t, check, files)
	}
}

func TestSharedChannelDirection(t *testing.T) {
	t.Parallel()
	for _, broken := range []bool{false, true} {
		files := coptest.ProgramFiles{
			"sample.go": `package p
func read(c chan int) { <-c }
func captured(c chan int) { <-c }
func clone(s []int) []int { return append([]int(nil), s...) }
`,
			"sample_test.go":          "package p\nvar callback func(chan int) = captured\n",
			"external_test.go":        "package p_test\nfunc read(c chan int) { <-c }\n",
			"testonly/sample_test.go": "package p\nfunc read(c chan int) { <-c }\n",
		}
		if broken {
			files["sample_test.go"] += "var _ = missing\n"
		}
		check := prog.New(cop.Meta{Name: "test"}, func(p *prog.Pass) {
			registered := newProgramCops()
			warm := registeredSharedCop(t, registered, "Lint/SlicesClone")
			warm.Check(&prog.Pass{Cop: warm, Program: p.Program})
			shared := registeredSharedCop(t, registered, ChannelDirection.Name())
			got := &prog.Pass{Cop: shared, Program: p.Program}
			want := &prog.Pass{Cop: ChannelDirection, Program: p.Program}
			shared.Check(got)
			ChannelDirection.Check(want)
			assert.ElementsMatch(t, want.Offenses(), got.Offenses())
			if broken {
				assert.Empty(t, got.Offenses())
			} else {
				assert.Len(t, got.Offenses(), 3)
			}
		})
		coptest.RunProgram(t, check, files)
	}
}

func TestTypedFileLoadErrors(t *testing.T) {
	t.Parallel()
	load := func(*prog.Pass) ([]*packages.Package, error) { return nil, errors.New("load failed") }
	for _, tc := range []struct {
		skipIllTyped bool
		message      string
	}{
		{true, "cannot inspect modernization candidates: load failed"},
		{false, "cannot inspect packages: load failed"},
	} {
		c := withTypedFiles(load, rubocops.NewLintSlicesClone, tc.skipIllTyped)
		p := &prog.Pass{Cop: c, Program: &prog.Program{}}
		c.Check(p)
		require.Len(t, p.Offenses(), 1)
		assert.Equal(t, tc.message, p.Offenses()[0].Message)
	}
}

func BenchmarkTypedPackageLoader(b *testing.B) {
	program := typedPackageFixture(b)
	pass := &prog.Pass{Program: program}
	b.Run("uncached", func(b *testing.B) {
		for b.Loop() {
			_, err := loadTypedPackages(pass)
			require.NoError(b, err)
		}
	})
	b.Run("shared", func(b *testing.B) {
		var loader typedPackageLoader
		_, err := loader.load(pass)
		require.NoError(b, err)
		for b.Loop() {
			_, err := loader.load(pass)
			require.NoError(b, err)
		}
	})
}

func typedPackageFixture(tb testing.TB) *prog.Program {
	tb.Helper()
	dir := tb.TempDir()
	for name, src := range map[string]string{
		"go.mod":         "module example.test\n\ngo 1.27\n",
		"sample.go":      "package p\n",
		"sample_test.go": "package p\nfunc testHelper() {}\n",
	} {
		require.NoError(tb, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
	}
	program, err := prog.LoadDir(dir, "./...")
	require.NoError(tb, err)
	return program
}
