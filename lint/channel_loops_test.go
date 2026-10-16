package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/dgageot/rubocop-go/config"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/dgageot/rubocop-go/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancellationLoop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		want       int
	}{
		{"cancel single assignment", `for { select { case done:=<-ctx.Done(): _=done; continue; case <-ch: } }`, 1},
		{"cancel comma ok", `for { select { case _,ok:=<-ctx.Done(): _=ok; break; case <-ch: } }`, 1},
		{"parenthesized context", `for { select { case <-(ctx.Done()): ctx=other; case <-ch: } }`, 0},
		{"parenthesized method", `for { select { case <-(ctx.Done)(): ctx=other; case <-ch: } }`, 0},
		{"recreated context", `for { var ctx,cancel=context.WithCancel(ctx); select { case <-ctx.Done(): cancel(); break; case <-ch: }; cancel() }`, 0},
		{"break select", `for { select { case <-ctx.Done(): break; case <-ch: } }`, 1},
		{"continue loop", `for { select { case <-ctx.Done(): continue; case <-ch: } }`, 1},
		{"fallthrough", `for { select { case <-ctx.Done(): println("cancelled"); case <-ch: } }`, 1},
		{"nested break", `for { select { case <-ctx.Done(): if yes { break }; case <-ch: } }`, 1},
		{"safe return", `for { select { case <-ctx.Done(): return; case <-ch: } }`, 0},
		{"safe labelled break", `outer: for { select { case <-ctx.Done(): break outer; case <-ch: } }`, 0},
		{"safe after select", `for { select { case <-ctx.Done(): break; case <-ch: }; return }`, 0},
		{"safe conditional after select", `for { select { case <-ctx.Done(): break; case <-ch: }; if yes { return } }`, 1},
		{"conditional loop", `for yes { select { case <-ctx.Done(): yes=false; case <-ch: } }`, 0},
		{"replace context", `for { select { case <-ctx.Done(): ctx = other; case <-ch: } }`, 0},
		{"panic", `for { select { case <-ctx.Done(): panic("stop"); case <-ch: } }`, 0},
		{"select once", `select { case <-ctx.Done(): break; case <-ch: }`, 0},
		{"nested closure", `go func(){ for { select { case <-ctx.Done(): break; case <-ch: } } }()`, 1},
		{"closure not loop", `for { go func(){ select { case <-ctx.Done(): break; case <-ch: } }(); return }`, 0},
		{"range loop", `for range ch { select { case <-ctx.Done(): break; default: } }`, 0},
		{"shadowed context", `{ ctx := fake{}; for { select { case <-ctx.Done(): break; case <-ch: } } }`, 0},
		{"nested loop exits", `for { select { case <-ctx.Done(): for { break }; return; case <-ch: } }`, 0},
	}
	files := coptest.ProgramFiles{}
	for i, tc := range tests {
		files[fmt.Sprintf("case%d/sample.go", i)] = `package p
import "context"
type fake struct{}
func(fake) Done() <-chan struct{} { return nil }
func f(ctx,other context.Context, ch chan int, yes bool) { _=ctx; _=other; ` + tc.body + ` }`
	}
	offenses := coptest.RunProgram(t, CancellationLoop, files)
	counts := make(map[string]int)
	for _, offense := range offenses {
		assert.Equal(t, "Lint/CancellationLoop", offense.CopName)
		counts[filepath.Base(filepath.Dir(offense.Pos.Filename))]++
	}
	for i, tc := range tests {
		assert.Equal(t, tc.want, counts["case"+strconv.Itoa(i)], tc.name)
	}
}

func TestClosedChannelLoop(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		want       int
	}{
		{"var receive", `for { var _,ok = <-ch; if !ok { continue } }`, 1},
		{"var receive return", `for { var _,ok = <-ch; if !ok { return } }`, 0},
		{"switch false safe", `for { _,ok:=<-ch; switch false { case ok: return; default: continue } }`, 0},
		{"switch ok unsafe", `for { _,ok:=<-ch; switch ok { case false: continue; default: return } }`, 1},
		{"switch ok safe", `for { _,ok:=<-ch; switch ok { case false: return; default: continue } }`, 0},
		{"switch false unsafe", `for { _,ok:=<-ch; switch false { case ok: continue; default: return } }`, 1},
		{"nested select default", `for { _,ok:=<-ch; if !ok { select { case ch=<-replacements: return; default: continue } } }`, 1},
		{"nested select ok write", `for { _,ok:=<-ch; if !ok { select { case ok=<-flags: return; default: continue } } }`, 1},
		{"recreated channel var", `for { var ch=make(chan int); close(ch); _,ok:=<-ch; if !ok { continue } }`, 0},
		{"recreated channel short", `for { ch:=make(chan int); close(ch); _,ok:=<-ch; if !ok { continue } }`, 0},
		{"continue", `for { select { case _,ok:=<-ch: if !ok { continue }; default: } }`, 1},
		{"break select", `for { select { case _,ok:=<-ch: if !ok { break }; default: } }`, 1},
		{"fallthrough", `for { select { case _,ok:=<-ch: if !ok { println("closed") }; default: } }`, 1},
		{"direct receive", `for { _,ok:=<-ch; if !ok { continue } }`, 1},
		{"return", `for { select { case _,ok:=<-ch: if !ok { return }; default: } }`, 0},
		{"labelled break", `outer: for { select { case _,ok:=<-ch: if !ok { break outer }; default: } }`, 0},
		{"direct break", `for { _,ok:=<-ch; if !ok { break } }`, 0},
		{"disable", `for { select { case _,ok:=<-ch: if !ok { ch=nil; continue }; default: } }`, 0},
		{"replace", `for { select { case _,ok:=<-ch: if !ok { ch=other }; default: } }`, 0},
		{"unrelated channel nil", `for { select { case _,ok:=<-ch: if !ok { other=nil; continue }; default: } }`, 1},
		{"shadowed nil", `for { select { case _,ok:=<-ch: if !ok { ch:=other; _=ch; continue }; default: } }`, 1},
		{"positive check", `for { select { case _,ok:=<-ch: if ok { println("item") } else { continue }; default: } }`, 1},
		{"positive check return", `for { select { case _,ok:=<-ch: if ok { continue } else { return }; default: } }`, 0},
		{"equals false", `for { select { case _,ok:=<-ch: if ok==false { continue }; default: } }`, 1},
		{"not equals true", `for { select { case _,ok:=<-ch: if true!=ok { continue }; default: } }`, 1},
		{"unchecked", `for { select { case _,ok:=<-ch: _=ok; default: } }`, 0},
		{"single receive", `for { select { case <-ch: continue; default: } }`, 0},
		{"conditional loop", `for yes { _,ok:=<-ch; if !ok { yes=false } }`, 0},
		{"after select break", `for { select { case _,ok:=<-ch: if !ok { break }; default: }; break }`, 0},
		{"closure", `go func(){ for { _,ok:=<-ch; if !ok { continue } } }()`, 1},
		{"panic", `for { _,ok:=<-ch; if !ok { panic("closed") } }`, 0},
		{"ok overwritten", `for { _,ok:=<-ch; ok=yes; if !ok { continue } }`, 0},
		{"nil only on one path", `for { _,ok:=<-ch; if !ok { if yes { ch=nil }; continue } }`, 1},
	}
	files := coptest.ProgramFiles{}
	for i, tc := range tests {
		files[fmt.Sprintf("case%d/sample.go", i)] = `package p
func f(ch,other chan int, yes bool, replacements chan chan int, flags chan bool) { _=other; ` + tc.body + ` }`
	}
	offenses := coptest.RunProgram(t, ClosedChannelLoop, files)
	counts := make(map[string]int)
	for _, offense := range offenses {
		assert.Equal(t, "Lint/ClosedChannelLoop", offense.CopName)
		counts[filepath.Base(filepath.Dir(offense.Pos.Filename))]++
	}
	for i, tc := range tests {
		assert.Equal(t, tc.want, counts["case"+strconv.Itoa(i)], tc.name)
	}
}

func TestChannelLoopRunner(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const body = `package p
func f(ch chan int) { for { _,ok:=<-ch; if !ok { continue } } }
`
	for name, content := range map[string]string{
		"go.mod":                  "module example.test\n\ngo 1.27\n",
		"regular/sample.go":       body,
		"tests/sample_test.go":    body,
		"pkg/config/v0/sample.go": body,
		"generated/sample.go":     "// Code generated by fixture; DO NOT EDIT.\n" + body,
		"suppressed/sample.go":    "//rubocop:disable-file Lint/ClosedChannelLoop\n" + body,
	} {
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0o700))
		require.NoError(t, os.WriteFile(name, []byte(content), 0o600))
	}
	var output bytes.Buffer
	r := runner.New(nil, config.DefaultConfig(), &output).WithProgramCops([]prog.Cop{ClosedChannelLoop})
	count, err := r.Run([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, 2, count, output.String())
}
