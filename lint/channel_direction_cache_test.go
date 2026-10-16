package main

import (
	"fmt"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChannelDirectionPositionKeys(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	first := fset.AddFile("source.go", -1, 20)
	variant := fset.AddFile("source.go", -1, 20)
	other := fset.AddFile("other.go", -1, 20)
	first.AddLineColumnInfo(5, "remapped.go", 100, 1)
	a := &channelAnalysis{fset: fset, keys: make(map[token.Pos]string)}
	for _, pos := range []token.Pos{first.Pos(5), first.Pos(20), variant.Pos(5), other.Pos(5), token.NoPos, other.Pos(20) + 100} {
		physical := fset.PositionFor(pos, false)
		want := fmt.Sprintf("%s:%d", physical.Filename, physical.Offset)
		assert.Equal(t, want, a.key(pos))
		assert.Equal(t, want, a.key(pos))
		assert.Equal(t, want, a.keys[pos])
	}
	assert.Equal(t, "source.go:5", a.key(first.Pos(5)))
	assert.Equal(t, a.key(first.Pos(5)), a.key(variant.Pos(5)))
	assert.NotEqual(t, a.key(first.Pos(5)), a.key(other.Pos(5)))

	freshSet := token.NewFileSet()
	freshFile := freshSet.AddFile("fresh.go", -1, 20)
	fresh := &channelAnalysis{fset: freshSet, keys: make(map[token.Pos]string)}
	assert.Equal(t, "fresh.go:5", fresh.key(freshFile.Pos(5)))
}
