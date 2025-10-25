package environment

import (
	"context"
	"fmt"
	"os"
	"slices"
)

func ExpandAll(ctx context.Context, values []string, env Provider) ([]string, error) {
	var expandedEnv []string

	for _, value := range values {
		expanded, err := Expand(ctx, value, env)
		if err != nil {
			return nil, err
		}

		expandedEnv = append(expandedEnv, expanded)
	}

	return expandedEnv, nil
}

func Expand(ctx context.Context, value string, env Provider) (string, error) {
	var err error

	expanded := os.Expand(value, func(name string) string {
		v := env.Get(ctx, name)
		if v == "" {
			err = fmt.Errorf("environment variable %q not set", name)
		}
		return v
	})
	if err != nil {
		return "", err
	}

	return expanded, nil
}

// ExpandLenient expands environment variables without returning errors for undefined variables.
// Undefined variables expand to empty strings, similar to standard shell behavior.
func ExpandLenient(ctx context.Context, value string, env Provider) string {
	return os.Expand(value, func(name string) string {
		return env.Get(ctx, name)
	})
}

func ToValues(envMap map[string]string) []string {
	var values []string
	for k, v := range envMap {
		values = append(values, fmt.Sprintf("%s=%s", k, v))
	}
	slices.Sort(values)
	return values
}
