package types

// Commands represents a set of named prompts for quick-starting conversations.
// It supports two YAML formats:
//
// commands:
//
//	df: "check disk space"
//	ls: "list files"
//
// or
//
// commands:
//   - df: "check disk space"
//   - ls: "list files"
type Commands map[string]string

// UnmarshalYAML supports both map and list-of-singleton-maps syntaxes.
func (c *Commands) UnmarshalYAML(unmarshal func(any) error) error {
	// Try direct map first
	var m map[string]string
	if err := unmarshal(&m); err == nil && m != nil {
		*c = m
		return nil
	}

	// Try list of maps [{k:v}, {k:v}]
	var list []map[string]string
	if err := unmarshal(&list); err == nil && list != nil {
		result := make(map[string]string)
		for _, item := range list {
			for k, v := range item {
				result[k] = v
			}
		}
		*c = result
		return nil
	}

	// If none matched, treat as empty
	*c = map[string]string{}
	return nil
}
