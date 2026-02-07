package latest

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemaFile is the path to the JSON schema file relative to the repo root.
const schemaFile = "../../../cagent-schema.json"

// jsonSchema mirrors the subset of JSON Schema we need for comparison.
type jsonSchema struct {
	Properties           map[string]jsonSchema `json:"properties,omitempty"`
	Definitions          map[string]jsonSchema `json:"definitions,omitempty"`
	Ref                  string                `json:"$ref,omitempty"`
	Items                *jsonSchema           `json:"items,omitempty"`
	AdditionalProperties any                   `json:"additionalProperties,omitempty"`
}

// resolveRef follows a $ref like "#/definitions/Foo" and returns the
// referenced schema. When no $ref is present it returns the receiver unchanged.
func (s jsonSchema) resolveRef(root jsonSchema) jsonSchema {
	if s.Ref == "" {
		return s
	}
	const prefix = "#/definitions/"
	if !strings.HasPrefix(s.Ref, prefix) {
		return s
	}
	name := strings.TrimPrefix(s.Ref, prefix)
	if def, ok := root.Definitions[name]; ok {
		return def
	}
	return s
}

// structJSONFields returns the set of JSON property names declared on a Go
// struct type via `json:"<name>,…"` tags. Fields tagged with `json:"-"` are
// excluded. It recurses into anonymous (embedded) struct fields so that
// promoted fields are included.
func structJSONFields(t reflect.Type) map[string]bool {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	fields := make(map[string]bool)
	for i := range t.NumField() {
		f := t.Field(i)

		// Recurse into anonymous (embedded) structs.
		if f.Anonymous {
			for k, v := range structJSONFields(f.Type) {
				fields[k] = v
			}
			continue
		}

		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

// schemaProperties returns the set of property names from a JSON schema
// definition. It does NOT follow $ref on individual properties – it only
// looks at the top-level "properties" map.
func schemaProperties(def jsonSchema) map[string]bool {
	props := make(map[string]bool, len(def.Properties))
	for k := range def.Properties {
		props[k] = true
	}
	return props
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestSchemaMatchesGoTypes verifies that every JSON-tagged field in the Go
// config structs has a corresponding property in cagent-schema.json (and
// vice-versa). This prevents the schema from silently drifting out of sync
// with the Go types.
func TestSchemaMatchesGoTypes(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(schemaFile)
	require.NoError(t, err, "failed to read schema file – run this test from the repo root")

	var root jsonSchema
	require.NoError(t, json.Unmarshal(data, &root))

	// mapping maps a JSON Schema definition name (or pseudo-name for inline
	// schemas) to the corresponding Go type. For top-level definitions that
	// live in the "definitions" section of the schema we use their exact
	// name. For schemas inlined inside a parent property we use
	// "Parent.property" as the key.
	type entry struct {
		goType     reflect.Type
		schemaDef  jsonSchema
		schemaName string // human-readable name for error messages
	}

	entries := []entry{
		// Top-level Config
		{reflect.TypeOf(Config{}), root, "Config (top-level)"},
	}

	// Definitions that map 1:1 to a Go struct.
	definitionMap := map[string]reflect.Type{
		"AgentConfig":           reflect.TypeOf(AgentConfig{}),
		"FallbackConfig":        reflect.TypeOf(FallbackConfig{}),
		"ModelConfig":           reflect.TypeOf(ModelConfig{}),
		"Metadata":              reflect.TypeOf(Metadata{}),
		"ProviderConfig":        reflect.TypeOf(ProviderConfig{}),
		"Toolset":               reflect.TypeOf(Toolset{}),
		"Remote":                reflect.TypeOf(Remote{}),
		"SandboxConfig":         reflect.TypeOf(SandboxConfig{}),
		"ScriptShellToolConfig": reflect.TypeOf(ScriptShellToolConfig{}),
		"PostEditConfig":        reflect.TypeOf(PostEditConfig{}),
		"PermissionsConfig":     reflect.TypeOf(PermissionsConfig{}),
		"HooksConfig":           reflect.TypeOf(HooksConfig{}),
		"HookMatcherConfig":     reflect.TypeOf(HookMatcherConfig{}),
		"HookDefinition":        reflect.TypeOf(HookDefinition{}),
		"RoutingRule":           reflect.TypeOf(RoutingRule{}),
		"ApiConfig":             reflect.TypeOf(APIToolConfig{}),
	}

	for name, goType := range definitionMap {
		def, ok := root.Definitions[name]
		require.True(t, ok, "schema definition %q not found", name)
		entries = append(entries, entry{goType, def, name})
	}

	// Inline schemas that don't have their own top-level definition but are
	// nested inside a parent property.
	type inlineEntry struct {
		goType reflect.Type
		// path navigates from a schema definition to the inline object,
		// e.g. []string{"RAGConfig", "results"} → definitions.RAGConfig.properties.results
		path []string
		name string
	}

	inlines := []inlineEntry{
		{reflect.TypeOf(StructuredOutput{}), []string{"AgentConfig", "structured_output"}, "StructuredOutput (AgentConfig.structured_output)"},
		{reflect.TypeOf(RAGConfig{}), []string{"RAGConfig"}, "RAGConfig"},
		{reflect.TypeOf(RAGToolConfig{}), []string{"RAGConfig", "tool"}, "RAGToolConfig (RAGConfig.tool)"},
		{reflect.TypeOf(RAGResultsConfig{}), []string{"RAGConfig", "results"}, "RAGResultsConfig (RAGConfig.results)"},
		{reflect.TypeOf(RAGFusionConfig{}), []string{"RAGConfig", "results", "fusion"}, "RAGFusionConfig (RAGConfig.results.fusion)"},
		{reflect.TypeOf(RAGRerankingConfig{}), []string{"RAGConfig", "results", "reranking"}, "RAGRerankingConfig (RAGConfig.results.reranking)"},
		{reflect.TypeOf(RAGChunkingConfig{}), []string{"RAGConfig", "strategies", "*", "chunking"}, "RAGChunkingConfig (RAGConfig.strategies[].chunking)"},
	}

	for _, il := range inlines {
		def := navigateSchema(t, root, il.path)
		entries = append(entries, entry{il.goType, def, il.name})
	}

	// Now compare each entry.
	for _, e := range entries {
		goFields := structJSONFields(e.goType)
		schemaProps := schemaProperties(e.schemaDef)

		missingInSchema := diff(goFields, schemaProps)
		missingInGo := diff(schemaProps, goFields)

		assert.Empty(t, sortedKeys(missingInSchema),
			"%s: Go struct has JSON fields not present in the schema", e.schemaName)
		assert.Empty(t, sortedKeys(missingInGo),
			"%s: schema has properties not present in the Go struct", e.schemaName)
	}
}

// navigateSchema walks from a top-level definition through nested properties.
// path[0] is the definition name; subsequent elements are property names.
// The special element "*" dereferences an array's "items" schema.
func navigateSchema(t *testing.T, root jsonSchema, path []string) jsonSchema {
	t.Helper()
	require.NotEmpty(t, path)

	cur, ok := root.Definitions[path[0]]
	require.True(t, ok, "definition %q not found", path[0])

	// Resolve top-level $ref if present.
	cur = cur.resolveRef(root)

	for _, segment := range path[1:] {
		if segment == "*" {
			require.NotNil(t, cur.Items, "expected items schema at %v", path)
			cur = *cur.Items
			cur = cur.resolveRef(root)
			continue
		}
		prop, ok := cur.Properties[segment]
		require.True(t, ok, "property %q not found at %v", segment, path)
		prop = prop.resolveRef(root)
		cur = prop
	}
	return cur
}

// diff returns keys present in a but not in b.
func diff(a, b map[string]bool) map[string]bool {
	d := make(map[string]bool)
	for k := range a {
		if !b[k] {
			d[k] = true
		}
	}
	return d
}
