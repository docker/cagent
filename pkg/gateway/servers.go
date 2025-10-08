package gateway

import (
	"strings"
)

func ParseServerRef(ref string) string {
	return strings.TrimPrefix(ref, "docker:")
}
