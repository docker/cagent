package markdown

type Theme struct {
	Text               string
	Hyperlink          string
	Heading            string
	CodeBlock          string
	CodeSpanForeground string
	CodeSpanBackground string
	Glow               string
}

func DarkTheme() *Theme {
	return &Theme{
		Text:               "#ffffff",
		Hyperlink:          "#3b8eea",
		Heading:            "35",
		CodeBlock:          "",
		CodeSpanForeground: "203",
		CodeSpanBackground: "236",
		Glow:               "35",
	}
}
