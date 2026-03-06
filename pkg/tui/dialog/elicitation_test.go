package dialog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseElicitationSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		schema         any
		expectedFields []ElicitationField
	}{
		{
			name:           "nil schema",
			schema:         nil,
			expectedFields: nil,
		},
		{
			name:           "empty schema",
			schema:         map[string]any{},
			expectedFields: nil,
		},
		{
			name: "schema with string property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{
						"type":        "string",
						"description": "Your username",
					},
				},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "username",
					Type:        "string",
					Description: "Your username",
					Required:    false,
				},
			},
		},
		{
			name: "schema with required fields",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"email": map[string]any{
						"type":        "string",
						"description": "Your email address",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Your name",
					},
				},
				"required": []any{"email"},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "email",
					Type:        "string",
					Description: "Your email address",
					Required:    true,
				},
				{
					Name:        "name",
					Type:        "string",
					Description: "Your name",
					Required:    false,
				},
			},
		},
		{
			name: "schema with boolean property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"remember_me": map[string]any{
						"type":        "boolean",
						"description": "Remember this device",
						"default":     true,
					},
				},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "remember_me",
					Type:        "boolean",
					Description: "Remember this device",
					Required:    false,
					Default:     true,
				},
			},
		},
		{
			name: "schema with number property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{
						"type":        "integer",
						"description": "Number of items",
						"default":     float64(10),
					},
				},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "count",
					Type:        "integer",
					Description: "Number of items",
					Required:    false,
					Default:     float64(10),
				},
			},
		},
		{
			name: "schema with enum property",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"color": map[string]any{
						"type":        "string",
						"description": "Choose a color",
						"enum":        []any{"red", "green", "blue"},
					},
				},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "color",
					Type:        "enum",
					Description: "Choose a color",
					Required:    false,
					EnumValues:  []string{"red", "green", "blue"},
				},
			},
		},
		{
			name: "schema with multiple properties sorted",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"zebra": map[string]any{
						"type": "string",
					},
					"apple": map[string]any{
						"type": "string",
					},
					"required_field": map[string]any{
						"type": "string",
					},
				},
				"required": []any{"required_field"},
			},
			expectedFields: []ElicitationField{
				{
					Name:     "required_field",
					Type:     "string",
					Required: true,
				},
				{
					Name:     "apple",
					Type:     "string",
					Required: false,
				},
				{
					Name:     "zebra",
					Type:     "string",
					Required: false,
				},
			},
		},
		{
			name: "schema with property title used for display",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_email": map[string]any{
						"type":        "string",
						"title":       "Email Address",
						"description": "Your email",
					},
				},
			},
			expectedFields: []ElicitationField{
				{
					Name:        "user_email",
					Title:       "Email Address",
					Type:        "string",
					Description: "Your email",
					Required:    false,
				},
			},
		},
		// Primitive schema tests
		{
			name: "primitive string schema with title",
			schema: map[string]any{
				"type":        "string",
				"title":       "Display Name",
				"description": "Your display name",
				"minLength":   float64(3),
				"maxLength":   float64(50),
				"default":     "user@example.com",
			},
			expectedFields: []ElicitationField{
				{
					Name:        "Display Name",
					Title:       "Display Name",
					Type:        "string",
					Description: "Your display name",
					Required:    true, // primitive schemas are implicitly required
					Default:     "user@example.com",
					MinLength:   3,
					MaxLength:   50,
				},
			},
		},
		{
			name: "primitive string schema without title",
			schema: map[string]any{
				"type":        "string",
				"description": "Enter a value",
			},
			expectedFields: []ElicitationField{
				{
					Name:        "value", // fallback name
					Type:        "string",
					Description: "Enter a value",
					Required:    true,
				},
			},
		},
		{
			name: "primitive boolean schema",
			schema: map[string]any{
				"type":    "boolean",
				"title":   "Accept Terms",
				"default": true,
			},
			expectedFields: []ElicitationField{
				{
					Name:     "Accept Terms",
					Title:    "Accept Terms",
					Type:     "boolean",
					Required: true,
					Default:  true,
				},
			},
		},
		{
			name: "primitive integer schema",
			schema: map[string]any{
				"type":    "integer",
				"title":   "Age",
				"default": float64(25),
			},
			expectedFields: []ElicitationField{
				{
					Name:     "Age",
					Title:    "Age",
					Type:     "integer",
					Required: true,
					Default:  float64(25),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fields := parseElicitationSchema(tt.schema)
			assert.Equal(t, tt.expectedFields, fields)
		})
	}
}

func TestNewElicitationDialog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		schema  any
	}{
		{
			name:    "simple dialog without fields",
			message: "Please confirm this action",
			schema:  nil,
		},
		{
			name:    "dialog with form fields",
			message: "Please enter your credentials",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"username": map[string]any{"type": "string", "description": "Your username"},
					"password": map[string]any{"type": "string", "description": "Your password"},
				},
				"required": []any{"username", "password"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dialog := NewElicitationDialog(tt.message, tt.schema, nil)
			require.NotNil(t, dialog)

			ed, ok := dialog.(*ElicitationDialog)
			require.True(t, ok)
			assert.Equal(t, tt.message, ed.message)
		})
	}
}

func TestElicitationDialog_collectAndValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		schema        any
		setupInputs   func(*ElicitationDialog)
		expectedValid bool
		expectedKeys  []string
	}{
		{
			name:          "no fields",
			schema:        nil,
			expectedValid: true,
		},
		{
			name: "required field empty",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
			expectedValid: false,
		},
		{
			name: "required field filled",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("test_name") },
			expectedValid: true,
			expectedKeys:  []string{"name"},
		},
		{
			name: "boolean field",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"enabled": map[string]any{"type": "boolean"}},
			},
			setupInputs:   func(d *ElicitationDialog) { d.boolValues[0] = true },
			expectedValid: true,
			expectedKeys:  []string{"enabled"},
		},
		{
			name: "invalid integer",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"count": map[string]any{"type": "integer"}},
				"required":   []any{"count"},
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("not_a_number") },
			expectedValid: false,
		},
		{
			name: "valid integer",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"count": map[string]any{"type": "integer"}},
				"required":   []any{"count"},
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("42") },
			expectedValid: true,
			expectedKeys:  []string{"count"},
		},
		{
			name: "valid enum value",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"color": map[string]any{"type": "string", "enum": []any{"red", "green", "blue"}}},
				"required":   []any{"color"},
			},
			setupInputs:   func(d *ElicitationDialog) { d.enumIndexes[0] = 0 }, // select "red"
			expectedValid: true,
			expectedKeys:  []string{"color"},
		},
		{
			name: "minLength validation fails",
			schema: map[string]any{
				"type":      "string",
				"title":     "Name",
				"minLength": float64(5),
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("abc") }, // 3 chars, need 5
			expectedValid: false,
		},
		{
			name: "minLength validation passes",
			schema: map[string]any{
				"type":      "string",
				"title":     "Name",
				"minLength": float64(3),
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("abcde") },
			expectedValid: true,
			expectedKeys:  []string{"Name"},
		},
		{
			name: "email format validation fails",
			schema: map[string]any{
				"type":   "string",
				"title":  "Email",
				"format": "email",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("not-an-email") },
			expectedValid: false,
		},
		{
			name: "email format validation passes",
			schema: map[string]any{
				"type":   "string",
				"title":  "Email",
				"format": "email",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("test@example.com") },
			expectedValid: true,
			expectedKeys:  []string{"Email"},
		},
		{
			name: "uri format validation fails",
			schema: map[string]any{
				"type":   "string",
				"title":  "Website",
				"format": "uri",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("not-a-url") },
			expectedValid: false,
		},
		{
			name: "uri format validation passes",
			schema: map[string]any{
				"type":   "string",
				"title":  "Website",
				"format": "uri",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("https://example.com") },
			expectedValid: true,
			expectedKeys:  []string{"Website"},
		},
		{
			name: "date format validation passes",
			schema: map[string]any{
				"type":   "string",
				"title":  "Birthday",
				"format": "date",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("2024-01-15") },
			expectedValid: true,
			expectedKeys:  []string{"Birthday"},
		},
		{
			name: "pattern validation fails",
			schema: map[string]any{
				"type":    "string",
				"title":   "Code",
				"pattern": "^[A-Z]{3}$",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("abc") },
			expectedValid: false,
		},
		{
			name: "pattern validation passes",
			schema: map[string]any{
				"type":    "string",
				"title":   "Code",
				"pattern": "^[A-Z]{3}$",
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("ABC") },
			expectedValid: true,
			expectedKeys:  []string{"Code"},
		},
		{
			name: "number minimum validation fails",
			schema: map[string]any{
				"type":    "number",
				"title":   "Age",
				"minimum": float64(18),
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("15") },
			expectedValid: false,
		},
		{
			name: "number minimum validation passes",
			schema: map[string]any{
				"type":    "number",
				"title":   "Age",
				"minimum": float64(18),
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("25") },
			expectedValid: true,
			expectedKeys:  []string{"Age"},
		},
		{
			name: "number maximum validation fails",
			schema: map[string]any{
				"type":    "integer",
				"title":   "Count",
				"maximum": float64(100),
			},
			setupInputs:   func(d *ElicitationDialog) { d.inputs[0].SetValue("150") },
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dialog := NewElicitationDialog("test", tt.schema, nil).(*ElicitationDialog)
			if tt.setupInputs != nil {
				tt.setupInputs(dialog)
			}

			content, firstErrorIdx := dialog.collectAndValidate()
			valid := firstErrorIdx < 0
			assert.Equal(t, tt.expectedValid, valid)

			if valid && tt.expectedKeys != nil {
				for _, key := range tt.expectedKeys {
					assert.Contains(t, content, key)
				}
			}
		})
	}
}
