package builtin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/tools"
)

const petStoreSpec = `{
  "openapi": "3.0.0",
  "info": { "title": "Pet Store", "version": "1.0.0" },
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": { "type": "integer" },
            "description": "Maximum number of pets to return"
          }
        ],
        "responses": {
          "200": { "description": "A list of pets" }
        }
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create a pet",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "name": { "type": "string", "description": "The pet name" },
                  "tag": { "type": "string" }
                },
                "required": ["name"]
              }
            }
          }
        },
        "responses": {
          "201": { "description": "Pet created" }
        }
      }
    },
    "/pets/{petId}": {
      "get": {
        "operationId": "showPetById",
        "summary": "Info for a specific pet",
        "parameters": [
          {
            "name": "petId",
            "in": "path",
            "required": true,
            "schema": { "type": "string" },
            "description": "The id of the pet to retrieve"
          }
        ],
        "responses": {
          "200": { "description": "Expected response to a valid request" }
        }
      }
    }
  }
}`

func serveSpec(t *testing.T, spec string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	t.Cleanup(server.Close)
	return server
}

func toolByName(t *testing.T, toolsList []tools.Tool, name string) tools.Tool {
	t.Helper()
	for _, tl := range toolsList {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("tool %q not found", name)
	return tools.Tool{}
}

func callTool(t *testing.T, tool tools.Tool, args string) *tools.ToolCallResult {
	t.Helper()
	result, err := tool.Handler(t.Context(), tools.ToolCall{
		Function: tools.FunctionCall{Arguments: args},
	})
	require.NoError(t, err)
	return result
}

func TestOpenAPITool_Tools(t *testing.T) {
	t.Parallel()

	specServer := serveSpec(t, petStoreSpec)
	openAPI := NewOpenAPITool(specServer.URL+"/openapi.json", nil)

	toolsList, err := openAPI.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, toolsList, 3)

	listPets := toolByName(t, toolsList, "listPets")
	assert.Equal(t, "List all pets", listPets.Description)
	assert.Equal(t, "openapi", listPets.Category)
	assert.True(t, listPets.Annotations.ReadOnlyHint)

	createPet := toolByName(t, toolsList, "createPet")
	assert.Equal(t, "Create a pet", createPet.Description)
	assert.False(t, createPet.Annotations.ReadOnlyHint)

	showPet := toolByName(t, toolsList, "showPetById")
	assert.Equal(t, "Info for a specific pet", showPet.Description)
}

func TestOpenAPITool_ToolParameters(t *testing.T) {
	t.Parallel()

	specServer := serveSpec(t, petStoreSpec)
	openAPI := NewOpenAPITool(specServer.URL+"/openapi.json", nil)

	toolsList, err := openAPI.Tools(t.Context())
	require.NoError(t, err)

	listPets := toolByName(t, toolsList, "listPets")

	schema, ok := listPets.Parameters.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", schema["type"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	limitProp, ok := props["limit"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", limitProp["type"])
	assert.Equal(t, "Maximum number of pets to return", limitProp["description"])
}

func TestOpenAPITool_CallGET(t *testing.T) {
	t.Parallel()

	var receivedURL string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"name":"Fido"}]`))
	}))
	t.Cleanup(apiServer.Close)

	spec := `{
		"openapi": "3.0.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"servers": [{"url": "` + apiServer.URL + `"}],
		"paths": {
			"/pets": {
				"get": {
					"operationId": "listPets",
					"summary": "List pets",
					"parameters": [
						{"name": "limit", "in": "query", "schema": {"type": "integer"}}
					],
					"responses": { "200": {"description": "ok"} }
				}
			}
		}
	}`

	specServer := serveSpec(t, spec)
	toolsList, err := NewOpenAPITool(specServer.URL+"/openapi.json", nil).Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolsList, 1)

	result := callTool(t, toolsList[0], `{"limit": 10}`)

	assert.False(t, result.IsError)
	assert.JSONEq(t, `[{"id":1,"name":"Fido"}]`, result.Output)
	assert.Equal(t, "/pets?limit=10", receivedURL)
}

func TestOpenAPITool_CallPOST(t *testing.T) {
	t.Parallel()

	var receivedBody []byte
	var receivedMethod string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1,"name":"Fido"}`))
	}))
	t.Cleanup(apiServer.Close)

	spec := `{
		"openapi": "3.0.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"servers": [{"url": "` + apiServer.URL + `"}],
		"paths": {
			"/pets": {
				"post": {
					"operationId": "createPet",
					"summary": "Create a pet",
					"requestBody": {
						"content": {
							"application/json": {
								"schema": {
									"type": "object",
									"properties": {
										"name": {"type": "string"},
										"tag": {"type": "string"}
									},
									"required": ["name"]
								}
							}
						}
					},
					"responses": { "201": {"description": "created"} }
				}
			}
		}
	}`

	specServer := serveSpec(t, spec)
	toolsList, err := NewOpenAPITool(specServer.URL+"/openapi.json", nil).Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolsList, 1)

	result := callTool(t, toolsList[0], `{"body_name": "Fido", "body_tag": "dog"}`)

	assert.False(t, result.IsError)
	assert.Equal(t, http.MethodPost, receivedMethod)

	var body map[string]any
	require.NoError(t, json.Unmarshal(receivedBody, &body))
	assert.Equal(t, "Fido", body["name"])
	assert.Equal(t, "dog", body["tag"])
}

func TestOpenAPITool_PathParameters(t *testing.T) {
	t.Parallel()

	var receivedURL string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"123","name":"Fido"}`))
	}))
	t.Cleanup(apiServer.Close)

	spec := `{
		"openapi": "3.0.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"servers": [{"url": "` + apiServer.URL + `"}],
		"paths": {
			"/pets/{petId}": {
				"get": {
					"operationId": "getPet",
					"summary": "Get a pet",
					"parameters": [
						{"name": "petId", "in": "path", "required": true, "schema": {"type": "string"}}
					],
					"responses": { "200": {"description": "ok"} }
				}
			}
		}
	}`

	specServer := serveSpec(t, spec)
	toolsList, err := NewOpenAPITool(specServer.URL+"/openapi.json", nil).Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolsList, 1)

	result := callTool(t, toolsList[0], `{"petId": "123"}`)

	assert.False(t, result.IsError)
	assert.Equal(t, "/pets/123", receivedURL)
}

func TestOpenAPITool_CustomHeaders(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(apiServer.Close)

	spec := `{
		"openapi": "3.0.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"servers": [{"url": "` + apiServer.URL + `"}],
		"paths": {
			"/data": {
				"get": {
					"operationId": "getData",
					"summary": "Get data",
					"responses": { "200": {"description": "ok"} }
				}
			}
		}
	}`

	headers := map[string]string{
		"Authorization": "Bearer test-token",
		"X-Custom":      "custom-value",
	}
	specServer := serveSpec(t, spec)
	toolsList, err := NewOpenAPITool(specServer.URL+"/openapi.json", headers).Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolsList, 1)

	callTool(t, toolsList[0], `{}`)

	assert.Equal(t, "Bearer test-token", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "custom-value", receivedHeaders.Get("X-Custom"))
}

func TestOpenAPITool_ErrorResponse(t *testing.T) {
	t.Parallel()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	t.Cleanup(apiServer.Close)

	spec := `{
		"openapi": "3.0.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"servers": [{"url": "` + apiServer.URL + `"}],
		"paths": {
			"/missing": {
				"get": {
					"operationId": "getMissing",
					"summary": "Get missing resource",
					"responses": { "404": {"description": "not found"} }
				}
			}
		}
	}`

	specServer := serveSpec(t, spec)
	toolsList, err := NewOpenAPITool(specServer.URL+"/openapi.json", nil).Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, toolsList, 1)

	result := callTool(t, toolsList[0], `{}`)

	assert.True(t, result.IsError)
	assert.Contains(t, result.Output, "404")
}

func TestOpenAPITool_InvalidSpecURL(t *testing.T) {
	t.Parallel()

	_, err := NewOpenAPITool("http://127.0.0.1:1/nonexistent", nil).Tools(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch OpenAPI spec")
}

func TestOpenAPITool_Instructions(t *testing.T) {
	t.Parallel()

	instructions := NewOpenAPITool("https://example.com/openapi.json", nil).Instructions()

	assert.Contains(t, instructions, "OpenAPI")
	assert.Contains(t, instructions, "https://example.com/openapi.json")
}

func TestOpenAPITool_OpenAPI31NullType(t *testing.T) {
	t.Parallel()

	// OpenAPI 3.1 uses anyOf with {"type": "null"} for nullable fields.
	// This must not cause a validation failure.
	spec := `{
		"openapi": "3.1.0",
		"info": { "title": "Test", "version": "1.0.0" },
		"paths": {
			"/items": {
				"get": {
					"operationId": "listItems",
					"summary": "List items",
					"responses": {
						"200": {
							"description": "ok",
							"content": {
								"application/json": {
									"schema": {
										"type": "array",
										"items": { "$ref": "#/components/schemas/Item" }
									}
								}
							}
						}
					}
				}
			}
		},
		"components": {
			"schemas": {
				"Item": {
					"type": "object",
					"properties": {
						"name": { "type": "string" },
						"score": {
							"anyOf": [
								{ "type": "integer" },
								{ "type": "null" }
							]
						}
					},
					"required": ["name"]
				}
			}
		}
	}`

	specServer := serveSpec(t, spec)
	openAPI := NewOpenAPITool(specServer.URL+"/openapi.json", nil)

	toolsList, err := openAPI.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, toolsList, 1)
	assert.Equal(t, "listItems", toolsList[0].Name)
}

func TestSanitizeToolName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "listPets", "listPets"},
		{"with path separators", "/pets/{petId}", "pets_petId"},
		{"with dashes", "get-pet-by-id", "get_pet_by_id"},
		{"with dots", "api.v1.getPet", "api_v1_getPet"},
		{"leading slash", "/api/pets", "api_pets"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sanitizeToolName(tt.in))
		})
	}
}
