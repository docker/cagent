package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancellationSafeResultSend(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		want       int
	}{
		{"unguarded send", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"explicit zero capacity", `ch := make(chan int, 1-1); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"var declaration", `var ch = make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"channel alias", `ch := make(chan int); out := ch; go func(){ out <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"receive alias", `ch := make(chan int); out := ch; go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-out: }`, 1},
		{"goroutine argument", `ch := make(chan int); go func(out chan<- int){ out <- 1 }(ch); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"parentheses", `ch := make(chan int); go func(){ (ch) <- 1 }(); select { case <-(ctx.Done()): return; case <-((ch)): }`, 1},
		{"multiple result channels", `ch := make(chan int); errs := make(chan error); go func(){ defer close(ch); defer close(errs); if yes { ch <- 1 } else { errs <- nil } }(); select { case <-ctx.Done(): return; case <-ch: case <-errs: }`, 2},
		{"shadowed unrelated local", `ch := make(chan int); go func(){ { ch := make(chan int, 1); ch <- 2 }; ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"named channel", `type result chan int; ch := make(result); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"buffered one shot", `ch := make(chan int, 1); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"cancellation aware send", `ch := make(chan int); go func(){ select { case ch <- 1: case <-ctx.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"cancellation aware context alias", `ch := make(chan int); alias := ctx; go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"cancellation aware context argument", `ch := make(chan int); go func(ctx c.Context){ select { case ch <- 1: case <-ctx.Done(): } }(ctx); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"shadowed context does not guard", `ch := make(chan int); go func(ctx c.Context){ select { case ch <- 1: case <-ctx.Done(): } }(other); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"shadowed receive in goroutine", `ch := make(chan int); go func(){ ch := make(chan int, 1); ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"extra receiver through alias", `ch := make(chan int); alias := ch; go func(){ ch <- 1 }(); go func(){ <-alias }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"escaped through alias", `ch := make(chan int); alias := ch; consume(alias); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"range drain", `ch := make(chan int); defer func(){ for range ch {} }(); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"nonblocking send", `ch := make(chan int); go func(){ select { case ch <- 1: default: } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"other context does not guard send", `ch := make(chan int); go func(){ select { case ch <- 1: case <-other.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"extra receiver", `ch := make(chan int); go func(){ ch <- 1 }(); go func(){ <-ch }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"extra caller receive", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }; <-ch`, 0},
		{"deferred drain", `ch := make(chan int); defer func(){ <-ch }(); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"escaped to helper", `ch := make(chan int); consume(ch); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"escaped into container", `ch := make(chan int); _ = []chan int{ch}; go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"address escapes", `ch := make(chan int); _ = &ch; go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"reassigned channel", `ch := make(chan int); go func(){ ch <- 1 }(); ch = make(chan int); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"reassigned alias", `ch := make(chan int); out := ch; go func(){ out <- 1 }(); out = make(chan int); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"multiple producers", `ch := make(chan int); go func(){ ch <- 1 }(); go func(){ ch <- 2 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"nested nonproducer closure", `ch := make(chan int); go func(){ func(){ ch <- 1 }() }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"no cancellation", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ch: }`, 0},
		{"cancellation without return", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): case <-ch: }; <-ch`, 0},
		{"unrelated done method", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-fake.Done(): return; case <-ch: }`, 0},
		{"shadowed receiver channel", `ch := make(chan int); go func(){ ch <- 1 }(); { ch := make(chan int, 1); select { case <-ctx.Done(): return; case <-ch: } }`, 0},
		{"shadowed make", `make := func() chan int { return nil }; ch := make(); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"parameter channel", `go func(){ external <- 1 }(); select { case <-ctx.Done(): return; case <-external: }`, 0},
		{"second consumer cancellation", `ch := make(chan int); go func(){ select { case ch <- 1: case <-ctx.Done(): } }(); select { case <-ctx.Done(): return; case <-other.Done(): return; case <-ch: }`, 1},
		{"second producer cancellation", `ch := make(chan int); go func(){ select { case ch <- 1: case <-other.Done(): case <-ctx.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"all consumer cancellations guarded", `ch := make(chan int); go func(){ select { case ch <- 1: case <-other.Done(): case <-ctx.Done(): } }(); select { case <-ctx.Done(): return; case <-other.Done(): return; case <-ch: }`, 0},
		{"duplicate cancellation alias", `alias := ctx; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-alias.Done(): return; case <-ch: }`, 0},
		{"guard alias reassigned away", `alias := ctx; alias = other; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"guard alias reassigned to consumer", `alias := other; alias = ctx; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"argument snapshots context before reassignment", `ch := make(chan int); go func(c c.Context){ select { case ch <- 1: case <-c.Done(): } }(ctx); ctx = other; select { case <-ctx.Done(): return; case <-ch: }`, 1},
		{"separate alias retains copied context", `alias := ctx; ch := make(chan int); go func(c c.Context){ select { case ch <- 1: case <-c.Done(): } }(alias); alias = other; select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"multiple consumer exits only one report", `ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-other.Done(): return; case <-ch: }`, 1},
		{"uncertain branch alias skipped", `alias := ctx; if yes { alias = other }; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},

		{"identity context conversion", `alias := c.Context(ctx); ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"WithValue shares cancellation", `alias := c.WithValue(ctx, "key", "value"); ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"invoked closure mutates alias", `alias := other; func(){ alias = ctx }(); ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"derived context unknown", `alias, cancel := c.WithCancel(ctx); defer cancel(); ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"address escapes through assignment", `alias := other; ptr := &alias; *ptr = ctx; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"address escapes through declaration", `alias := other; var ptr = &alias; *ptr = ctx; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"address escapes through assigned call", `alias := other; _ = replace(&alias, ctx); ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},

		{"invoked closure in assignment", `alias := other; unused := func() int { alias = ctx; return 0 }(); _ = unused; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"invoked closure in declaration", `alias := other; var unused = func() int { alias = ctx; return 0 }(); _ = unused; ch := make(chan int); go func(){ select { case ch <- 1: case <-alias.Done(): } }(); select { case <-ctx.Done(): return; case <-ch: }`, 0},
		{"invoked closure in argument", `alias := other; ch := make(chan int); go func(_ int){ select { case ch <- 1: case <-alias.Done(): } }(func() int { alias = ctx; return 0 }()); select { case <-ctx.Done(): return; case <-ch: }`, 0},

		{"caller closure", `_ = func(){ ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: } }`, 1},
	}
	files := coptest.ProgramFiles{}
	for i, tc := range tests {
		files[fmt.Sprintf("case%d/sample.go", i)] = `package p
import c "context"
type fakeContext struct{}
func (fakeContext) Done() <-chan struct{} { return nil }
func consume(chan int) {}
func replace(dst *c.Context, ctx c.Context) bool { *dst = ctx; return true }
func f(ctx, other c.Context, external chan int, yes bool, fake fakeContext) { ` + tc.body + ` }
`
	}
	offenses := coptest.RunProgram(t, CancellationSafeResultSend, files)
	got := make(map[string]int)
	for _, o := range offenses {
		assert.Equal(t, "Lint/CancellationSafeResultSend", o.CopName)
		assert.Contains(t, o.Message, "context cancellation")
		got[filepath.Base(filepath.Dir(o.Pos.Filename))]++
	}
	for i, tc := range tests {
		assert.Equal(t, tc.want, got[fmt.Sprintf("case%d", i)], tc.name)
	}
}

func TestCancellationSafeResultSendScope(t *testing.T) {
	t.Parallel()
	const src = `package p
import "context"
func f(ctx context.Context) { ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: } }
`
	offenses := coptest.RunProgram(t, CancellationSafeResultSend, coptest.ProgramFiles{
		"pkg/config/v0/sample.go":     src,
		"pkg/config/v15/sample.go":    src,
		"pkg/config/latest/sample.go": src,
		"generated/sample.go":         "// Code generated by fixture; DO NOT EDIT.\n" + src,
		"tests/sample.go":             "package p\n",
		"tests/sample_test.go":        src,
	})
	require.Len(t, offenses, 1)
	assert.True(t, strings.HasSuffix(filepath.ToSlash(offenses[0].Pos.Filename), "/pkg/config/latest/sample.go"))
}

func TestCancellationSafeResultSendReadLine(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../pkg/input/readline.go")
	require.NoError(t, err)
	offenses := coptest.RunProgram(t, CancellationSafeResultSend, coptest.ProgramFiles{"input/readline.go": string(src)})
	assert.Empty(t, offenses)
}

func TestCancellationSafeResultSendSuppressions(t *testing.T) {
	testCorrectnessCopSuppressions(t, CancellationSafeResultSend, `package sample
import "context"
func f(ctx context.Context) { ch := make(chan int); go func(){ ch <- 1 }(); select { case <-ctx.Done(): return; case <-ch: } } // offense
`)
}
