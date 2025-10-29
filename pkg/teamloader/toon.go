package teamloader

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/alpkeskin/gotoon"

	"github.com/docker/cagent/pkg/tools"
)

type toonTools struct {
	tools.ToolSet
	toolRegexps []*regexp.Regexp
}

func (f *toonTools) Tools(ctx context.Context) ([]tools.Tool, error) {
	allTools, err := f.ToolSet.Tools(ctx)
	if err != nil {
		return nil, err
	}

	for i, tool := range allTools {
		for _, regex := range f.toolRegexps {
			if !regex.MatchString(tool.Name) {
				continue
			}

			handler := tool.Handler
			tool.Handler = func(ctx context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
				res, err := handler(ctx, toolCall)
				if err != nil {
					return res, err
				}

				var o map[string]any
				err = json.Unmarshal([]byte(res.Output), &o)
				if err != nil {
					return res, nil
				}

				tooned, err := gotoon.Encode(o)
				if err != nil {
					return res, err
				}

				res.Output = tooned
				return res, nil
			}
			allTools[i] = tool
		}
	}

	return allTools, nil
}

func WithToon(inner tools.ToolSet, toon string) tools.ToolSet {
	if toon == "" {
		return inner
	}

	var toolRegexps []*regexp.Regexp

	for toolName := range strings.SplitSeq(toon, ",") {
		toolRegexps = append(toolRegexps, regexp.MustCompile(strings.TrimSpace(toolName)))
	}
	return &toonTools{
		ToolSet:     inner,
		toolRegexps: toolRegexps,
	}
}
