package markdown

import "sync"

var renderer = sync.OnceValue(newRenderer)

func Render(input string, width int) string {
	return renderer().Render([]byte(input), min(width, 120))
}
