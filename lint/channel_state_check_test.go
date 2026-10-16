package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStateCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"for post guard", `func f(ch chan int) { for ;len(ch)>0;<-ch {} }`, 1},
		{"for post reassigned", `func f(ch,other chan int) { for ;len(ch)>0;<-ch { ch=other } }`, 0},
		{"goto skips guard", `func f(ch chan int) { goto read; if len(ch)==0 { return }; read: <-ch }`, 0},
		{"fallthrough guard", `func f(ch chan int) { switch { case len(ch)>0: fallthrough; default: <-ch } }`, 1},
		{"fallthrough mutation", `func f(ch,other chan int, ready bool) { if len(ch)>0 { switch { case ready: ch=other; fallthrough; default: <-ch } } }`, 0},
		{"for guard", `func f(ch chan int) { for len(ch)>0 { <-ch } }`, 1},
		{"short circuit", `func f(ch chan int) { if len(ch)>0 && <-ch>0 {} }`, 1},
		{"short circuit or", `func f(ch chan int) { if len(ch)==0 || <-ch>0 {} }`, 1},
		{"switch guard", `func f(ch chan int) { switch { case len(ch)>0: <-ch } }`, 1},
		{"short circuit unsafe alternative", `func f(ch chan int, ready bool) { if ready || len(ch)>0 && <-ch>0 {} }`, 1},
		{"send", `func f(ch chan int) { if len(ch) < cap(ch) { ch <- 1 } }`, 1},
		{"else if", `func f(ch chan int, ready bool) { if len(ch) == 0 {} else if ready { <-ch } }`, 1},
		{"if initializer", `func f(ch chan int) { if ch := ch; len(ch) > 0 { <-ch } }`, 1},
		{"loop", `func f(ch chan int) { if len(ch) > 0 { for { <-ch; break } } }`, 1},
		{"switch clause", `func f(ch chan int) { if len(ch) > 0 { switch { default: <-ch } } }`, 1},
		{"loop own guard", `func f(ch, other chan int) { for { if len(ch) > 0 { <-ch }; ch = other } }`, 1},
		{"same name independent scopes", `func f(ch chan int) { if len(ch) > 0 { <-ch }; { ch := make(chan int); <-ch } }`, 1},
		{"partial types", `var _ = missing; func f(ch chan int) { if len(ch) > 0 { <-ch } }`, 1},
		{"receive", `func f(ch chan int) { if len(ch) > 0 { <-ch } }`, 1},
		{"receive assignment", `func f(ch chan int) { if len(ch) > 0 { v, ok := <-ch; _, _ = v, ok } }`, 1},
		{"receive expression", `func f(ch chan int) int { if len(ch) > 0 { return 1 + <-ch }; return 0 }`, 1},
		{"reversed send", `func f(ch chan int) { if cap(ch) > len(ch) { ch <- 1 } }`, 1},
		{"reversed receive", `func f(ch chan int) { if 0 < len(ch) { <-ch } }`, 1},
		{"not empty", `func f(ch chan int) { if len(ch) != 0 { <-ch } }`, 1},
		{"not full", `func f(ch chan int) { if len(ch) != cap(ch) { ch <- 1 } }`, 1},
		{"minimum receive", `func f(ch chan int) { if len(ch) >= 1 { <-ch } }`, 1},
		{"exact receive", `func f(ch chan int) { if len(ch) == 2 { <-ch } }`, 1},
		{"constant receive", `const n = 1; func f(ch chan int) { if len(ch) >= n { <-ch } }`, 1},
		{"negated receive", `func f(ch chan int) { if !(len(ch) == 0) { <-ch } }`, 1},
		{"send else", `func f(ch chan int) { if len(ch) == cap(ch) {} else { ch <- 1 } }`, 1},
		{"send reversed else", `func f(ch chan int) { if cap(ch) <= len(ch) {} else { ch <- 1 } }`, 1},
		{"receive else", `func f(ch chan int) { if len(ch) == 0 {} else { <-ch } }`, 1},
		{"receive reversed else", `func f(ch chan int) { if 0 >= len(ch) {} else { <-ch } }`, 1},
		{"receive return guard", `func f(ch chan int) { if len(ch) == 0 { return }; <-ch }`, 1},
		{"send return guard", `func f(ch chan int) { if len(ch) >= cap(ch) { return }; ch <- 1 }`, 1},
		{"continue guard", `func f(ch chan int) { for { if len(ch) == 0 { continue }; <-ch } }`, 1},
		{"break guard", `func f(ch chan int) { for { if len(ch) == 0 { break }; <-ch } }`, 1},
		{"conjunction", `func f(ch chan int, ready bool) { if ready && len(ch) > 0 { <-ch } }`, 1},
		{"else disjunction", `func f(ch chan int, ready bool) { if !ready || len(ch) == 0 {} else { <-ch } }`, 1},
		{"parentheses", `func f(ch chan int) { if (len((ch))) < (cap((ch))) { (ch) <- 1 } }`, 1},
		{"named channel", `type queue chan int; func f(ch queue) { if len(ch) > 0 { <-ch } }`, 1},
		{"directional channels", `func f(in <-chan int, out chan<- int) { if len(in) > 0 { <-in }; if len(out) < cap(out) { out <- 1 } }`, 2},
		{"field", `type q struct { ch chan int }; func f(q *q) { if len(q.ch) > 0 { <-q.ch } }`, 1},
		{"pointer", `func f(ch *chan int) { if len(*ch) > 0 { <-*ch } }`, 1},
		{"nested if", `func f(ch chan int, ready bool) { if len(ch) > 0 { if ready { <-ch } } }`, 1},
		{"multiple operations", `func f(ch chan int) { if len(ch) > 0 { <-ch; <-ch } }`, 2},
		{"duplicate guards", `func f(ch chan int) { if len(ch) > 0 { if len(ch) > 0 { <-ch } } }`, 1},
		{"closure own guard", `func f(ch chan int) { go func() { if len(ch) > 0 { <-ch } }() }`, 1},
		{"blocking select receive", `func f(ch chan int) { if len(ch) > 0 { select { case <-ch: } } }`, 1},
		{"blocking select send", `func f(ch chan int) { if len(ch) < cap(ch) { select { case ch <- 1: } } }`, 1},
		{"select body", `func f(ch chan int) { if len(ch) > 0 { select { default: <-ch } } }`, 1},
		{"select send value blocks", `func f(ch chan int) { if len(ch) > 0 { select { case ch <- <-ch: default: } } }`, 1},
		{"select channel expression blocks", `func f(ch chan chan int) { if len(ch) > 0 { select { case <- (<-ch): default: } } }`, 1},
		{"goroutine argument", `func consume(int) {}; func f(ch chan int) { if len(ch) > 0 { go consume(<-ch) } }`, 1},
		{"deferred argument", `func consume(int) {}; func f(ch chan int) { if len(ch) > 0 { defer consume(<-ch) } }`, 1},
		{"slices", `func f(xs []int, ch chan int) { if len(xs) < cap(xs) { ch <- 1 }; if len(xs) > 0 { <-ch } }`, 0},
		{"maps", `func f(xs map[int]int, ch chan int) { if len(xs) > 0 { <-ch } }`, 0},
		{"different channel", `func f(a, b chan int) { if len(a) < cap(a) { b <- 1 }; if len(a) > 0 { <-b } }`, 0},
		{"different capacity", `func f(a, b chan int) { if len(a) < cap(b) { a <- 1 } }`, 0},
		{"different receiver", `type q struct { ch chan int }; func f(a, b *q) { if len(a.ch) > 0 { <-b.ch } }`, 0},
		{"shadowed len", `func f(ch chan int) { len := func(chan int) int { return 1 }; if len(ch) > 0 { <-ch } }`, 0},
		{"shadowed cap", `func f(ch chan int) { cap := func(chan int) int { return 1 }; if len(ch) < cap(ch) { ch <- 1 } }`, 0},
		{"shadowed channel", `func f(ch chan int) { if len(ch) > 0 { ch := make(chan int); <-ch } }`, 0},
		{"shadowed receiver", `type q struct { ch chan int }; func f(v *q) { if len(v.ch) > 0 { v := &q{}; <-v.ch } }`, 0},
		{"reassigned channel", `func f(ch, other chan int) { if len(ch) > 0 { ch = other; <-ch } }`, 0},
		{"reassigned field", `type q struct { ch chan int }; func f(v *q, ch chan int) { if len(v.ch) > 0 { v.ch = ch; <-v.ch } }`, 0},
		{"reassigned receiver", `type q struct { ch chan int }; func f(v, other *q) { if len(v.ch) > 0 { v = other; <-v.ch } }`, 0},
		{"channel alias", `func f(ch chan int) { other := ch; if len(ch) > 0 { <-other } }`, 0},
		{"nested goroutine", `func f(ch chan int) { if len(ch) > 0 { go func() { <-ch }() } }`, 0},
		{"nested function", `func f(ch chan int) { if len(ch) > 0 { fn := func() { <-ch }; fn() } }`, 0},
		{"immediate function", `func f(ch chan int) { if len(ch) > 0 { func() { <-ch }() } }`, 0},
		{"deferred function", `func f(ch chan int) { if len(ch) > 0 { defer func() { <-ch }() } }`, 0},
		{"nonblocking receive", `func f(ch chan int) { if len(ch) > 0 { select { case <-ch: default: } } }`, 0},
		{"nonblocking receive assignment", `func f(ch chan int) { if len(ch) > 0 { select { case v, ok := <-ch: _, _ = v, ok; default: } } }`, 0},
		{"nonblocking send", `func f(ch chan int) { if len(ch) < cap(ch) { select { case ch <- 1: default: } } }`, 0},
		{"full does not guard send", `func f(ch chan int) { if len(ch) == cap(ch) { ch <- 1 } }`, 0},
		{"empty does not guard receive", `func f(ch chan int) { if len(ch) == 0 { <-ch } }`, 0},
		{"possibly empty", `func f(ch chan int) { if len(ch) >= 0 { <-ch } }`, 0},
		{"possibly full", `func f(ch chan int) { if len(ch) <= cap(ch) { ch <- 1 } }`, 0},
		{"unknown receive bound", `func f(ch chan int, n int) { if len(ch) > n { <-ch } }`, 0},
		{"disjunction", `func f(ch chan int, ready bool) { if ready || len(ch) > 0 { <-ch } }`, 0},
		{"else conjunction", `func f(ch chan int, ready bool) { if ready && len(ch) == 0 {} else { <-ch } }`, 0},
		{"unrelated subsequent receive", `func f(ch chan int) { if len(ch) > 0 {}; <-ch }`, 0},
		{"conditional return", `func f(ch chan int, ready bool) { if len(ch) == 0 { if ready { return } }; <-ch }`, 0},
		{"repeated call", `func f(get func() chan int) { if len(get()) > 0 { <-get() } }`, 0},
		{"select receive reassignment", `func f(ch chan int, outer chan chan int) { if len(ch) > 0 { select { case ch = <-outer: <-ch; default: } } }`, 0},
		{"if initializer reassignment", `func f(ch, other chan int) { if len(ch) > 0 { if ch = other; true { <-ch } } }`, 0},
		{"switch initializer reassignment", `func f(ch, other chan int) { if len(ch) > 0 { switch ch = other; { default: <-ch } } }`, 0},
		{"loop reassignment", `func f(ch, other chan int) { if len(ch) > 0 { for { <-ch; ch = other } } }`, 0},
		{"loop initializer reassignment", `func f(ch, other chan int) { if len(ch) > 0 { for ch = other; ; { <-ch } } }`, 0},
		{"range reassignment", `func f(ch chan int, chs []chan int) { if len(ch) > 0 { for _, ch = range chs { <-ch } } }`, 0},
		{"pointer reassignment", `func f(ch *chan int, other chan int) { if len(*ch) > 0 { *ch = other; <-*ch } }`, 0},
		{"indexed channels", `func f(chs []chan int) { if len(chs[0]) > 0 { <-chs[0] } }`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			offenses := coptest.RunTyped(t, newChannelStateCheck(), "package p\n"+tc.src)
			require.Len(t, offenses, tc.want)
			for _, offense := range offenses {
				assert.Equal(t, "Lint/ChannelStateCheck", offense.CopName)
				assert.Contains(t, offense.Message, "does not reserve")
				assert.Contains(t, offense.Message, "select or explicit synchronization")
			}
		})
	}
}

func TestChannelStateCheckScope(t *testing.T) {
	t.Parallel()
	const src = "package p\nfunc f(ch chan int) { if len(ch) > 0 { <-ch } }"
	files := coptest.ProgramFiles{
		"pkg/config/v0/sample.go":     src,
		"pkg/config/v15/sample.go":    src,
		"pkg/config/latest/sample.go": src,
		"generated/sample.go":         "// Code generated by fixture; DO NOT EDIT.\n" + src,
	}
	offenses := coptest.RunProgram(t, prog.FromFile(newChannelStateCheck()), files)
	require.Len(t, offenses, 1)
	assert.True(t, strings.HasSuffix(filepath.ToSlash(offenses[0].Pos.Filename), "/pkg/config/latest/sample.go"))
	assert.Empty(t, coptest.Run(t, newChannelStateCheck(), src))
}

func TestChannelStateCheckRunner(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const src = "package p\nfunc f(ch chan int) { if len(ch) > 0 { <-ch } }\n"
	for name, content := range map[string]string{
		"go.mod":                       "module example.test\n\ngo 1.27\n",
		"reported/sample.go":           src,
		"inline/sample.go":             strings.ReplaceAll(src, " }\n", " } //rubocop:disable Lint/ChannelStateCheck\n"),
		"suppressed/sample.go":         "//rubocop:disable-file Lint/ChannelStateCheck\n" + src,
		"tests/sample.go":              "package p\n",
		"tests/sample_test.go":         src,
		"external/sample.go":           "package p\n",
		"external/sample_test.go":      strings.Replace(src, "package p", "package p_test", 1),
		"testonly/sample_test.go":      src,
		"generated/sample_test.go":     "// Code generated by fixture; DO NOT EDIT.\n" + src,
		"pkg/config/v1/sample_test.go": src,
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
		require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
	}
	var output bytes.Buffer
	// The production wrapper extracts this file cop from a program-cop constructor.
	wrapped := withTypedFiles(func(opts ...cop.FuncOption) *prog.Func { return prog.FromFile(newChannelStateCheck(opts...)) })
	r := runner.New(nil, config.DefaultConfig(), &output).WithProgramCops([]prog.Cop{wrapped})
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 4, count, output.String())
	for _, name := range []string{"reported/sample.go", "tests/sample_test.go", "external/sample_test.go", "testonly/sample_test.go"} {
		assert.Contains(t, filepath.ToSlash(output.String()), name)
		content, err := os.ReadFile(name)
		require.NoError(t, err)
		assert.Contains(t, string(content), "if len(ch) > 0 { <-ch }")
	}
}
