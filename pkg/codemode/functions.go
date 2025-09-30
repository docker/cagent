package codemode

import (
	"fmt"
	"slices"
	"strings"

	"github.com/docker/cagent/pkg/tools"
)

func toolToJsDoc(tool tools.Tool) string {
	var doc strings.Builder

	doc.WriteString("===== " + tool.Function.Name + " =====\n\n")
	doc.WriteString(strings.TrimSpace(tool.Function.Description))
	doc.WriteString("\n\n")
	if len(tool.Function.Parameters.Properties) == 0 {
		doc.WriteString(fmt.Sprintf("%s(): string\n", tool.Function.Name))
	} else {
		doc.WriteString(fmt.Sprintf("%s(args: ArgsObject): string\n", tool.Function.Name))
		doc.WriteString("\nwhere type ArgsObject = {\n")
		for paramName, param := range tool.Function.Parameters.Properties {
			pType := "Object"

			var (
				pDesc string
				pEnum string
			)
			if paramMap, ok := param.(map[string]any); ok {
				if t, ok := paramMap["type"].(string); ok {
					pType = t
				}
				if d, ok := paramMap["description"].(string); ok {
					pDesc = d
				}
				if values, ok := paramMap["enum"].([]any); ok {
					for _, v := range values {
						if pEnum != "" {
							pEnum += " | "
						}
						if pType == "string" {
							pEnum += fmt.Sprintf("'%v'", v)
						} else {
							pEnum += fmt.Sprintf("%v", v)
						}
					}
				}
			}

			if !slices.Contains(tool.Function.Parameters.Required, paramName) {
				paramName += "?"
			}

			if pEnum != "" {
				doc.WriteString(fmt.Sprintf("  %s: %s // %s\n", paramName, pEnum, strings.TrimSpace(pDesc)))
			} else {
				doc.WriteString(fmt.Sprintf("  %s: %s // %s\n", paramName, pType, strings.TrimSpace(pDesc)))
			}
		}
		doc.WriteString("};\n")
	}

	return doc.String()
}
