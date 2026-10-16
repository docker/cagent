package main

import (
	"testing"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/coptest"
	"github.com/dgageot/rubocop-go/prog"
	"github.com/stretchr/testify/assert"
)

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
			warm := registeredSharedCop(t, programCops, "Lint/SlicesClone")
			warm.Check(&prog.Pass{Cop: warm, Program: p.Program})
			shared := registeredSharedCop(t, programCops, ChannelDirection.Name())
			got := &prog.Pass{Cop: shared, Program: p.Program}
			shared.Check(got)
			if broken {
				assert.Empty(t, got.Offenses())
			} else {
				assert.Len(t, got.Offenses(), 3)
			}
		})
		coptest.RunProgram(t, check, files)
	}
}
