package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/docker/cagent/pkg/tools"
	"github.com/docker/cagent/pkg/upstream"
	"github.com/docker/cagent/pkg/useragent"
)

const httpTimeout = 30 * time.Second

// OpenAPITool generates HTTP tools from an OpenAPI specification.
type OpenAPITool struct {
	specURL string
	headers map[string]string
}

// Verify interface compliance.
var (
	_ tools.ToolSet      = (*OpenAPITool)(nil)
	_ tools.Instructable = (*OpenAPITool)(nil)
)

// NewOpenAPITool creates a new OpenAPI toolset from the given spec URL.
func NewOpenAPITool(specURL string, headers map[string]string) *OpenAPITool {
	return &OpenAPITool{
		specURL: specURL,
		headers: headers,
	}
}

// Instructions returns usage instructions for the OpenAPI toolset.
func (t *OpenAPITool) Instructions() string {
	return fmt.Sprintf(`## OpenAPI tools

These tools were generated from the OpenAPI specification at %s.
Each tool corresponds to an API endpoint. Use the tool parameters as described.`, t.specURL)
}

// Tools fetches and parses the OpenAPI specification, returning a tool for each operation.
func (t *OpenAPITool) Tools(ctx context.Context) ([]tools.Tool, error) {
	spec, err := t.fetchSpec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenAPI spec from %s: %w", t.specURL, err)
	}

	return t.buildTools(spec)
}

// fetchSpec retrieves and parses the OpenAPI specification from the configured URL.
func (t *OpenAPITool) fetchSpec(ctx context.Context) (*openapi3.T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.specURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	setHeaders(req, t.headers)

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, 10<<20) // 10MB limit
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if the spec was truncated.
	if len(body) >= 10<<20 {
		return nil, fmt.Errorf("OpenAPI spec exceeds 10MB size limit")
	}

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	spec, err := loader.LoadFromData(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	// Validate the spec but don't fail on validation errors.
	// Some valid OpenAPI 3.1 features (e.g. "type": "null") are not yet
	// supported by the validator in kin-openapi.
	if err := spec.Validate(loader.Context); err != nil {
		slog.Warn("OpenAPI spec validation reported issues; proceeding anyway", "url", t.specURL, "error", err)
	}

	return spec, nil
}

// buildTools converts an OpenAPI spec into a list of tools.
func (t *OpenAPITool) buildTools(spec *openapi3.T) ([]tools.Tool, error) {
	baseURL, err := t.resolveBaseURL(spec)
	if err != nil {
		return nil, err
	}

	var result []tools.Tool
	for path, pathItem := range spec.Paths.Map() {
		for method, op := range pathOperations(pathItem) {
			result = append(result, t.operationToTool(baseURL, path, method, op))
		}
	}

	return result, nil
}

// pathOperations returns all non-nil operations for a path item.
func pathOperations(item *openapi3.PathItem) map[string]*openapi3.Operation {
	all := map[string]*openapi3.Operation{
		http.MethodGet:     item.Get,
		http.MethodPost:    item.Post,
		http.MethodPut:     item.Put,
		http.MethodPatch:   item.Patch,
		http.MethodDelete:  item.Delete,
		http.MethodHead:    item.Head,
		http.MethodOptions: item.Options,
	}

	ops := make(map[string]*openapi3.Operation, len(all))
	for m, op := range all {
		if op != nil {
			ops[m] = op
		}
	}

	return ops
}

// resolveBaseURL determines the base URL for API requests.
func (t *OpenAPITool) resolveBaseURL(spec *openapi3.T) (string, error) {
	if len(spec.Servers) > 0 && spec.Servers[0].URL != "" {
		serverURL := spec.Servers[0].URL

		// Resolve relative server URLs against the spec URL.
		if !strings.HasPrefix(serverURL, "http://") && !strings.HasPrefix(serverURL, "https://") {
			specParsed, err := url.Parse(t.specURL)
			if err != nil {
				return "", fmt.Errorf("failed to parse spec URL: %w", err)
			}

			resolved, err := specParsed.Parse(serverURL)
			if err != nil {
				return "", fmt.Errorf("failed to resolve server URL: %w", err)
			}

			serverURL = resolved.String()
		}

		return strings.TrimRight(serverURL, "/"), nil
	}

	// Fall back to the spec URL's origin.
	specParsed, err := url.Parse(t.specURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse spec URL: %w", err)
	}

	return fmt.Sprintf("%s://%s", specParsed.Scheme, specParsed.Host), nil
}

// operationToTool converts a single OpenAPI operation to a tool.
func (t *OpenAPITool) operationToTool(baseURL, path, method string, op *openapi3.Operation) tools.Tool {
	name := operationToolName(path, method, op)
	desc := operationDescription(path, method, op)
	schema := operationSchema(op)

	readOnly := method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions

	return tools.Tool{
		Name:        name,
		Category:    "openapi",
		Description: desc,
		Parameters:  schema,
		Handler: tools.NewHandler((&openAPIHandler{
			baseURL: baseURL,
			path:    path,
			method:  method,
			headers: t.headers,
		}).callTool),
		Annotations: tools.ToolAnnotations{
			ReadOnlyHint: readOnly,
			Title:        desc,
		},
	}
}

// operationToolName returns a tool name derived from the operationId or the method+path.
func operationToolName(path, method string, op *openapi3.Operation) string {
	if op.OperationID != "" {
		return sanitizeToolName(op.OperationID)
	}

	return sanitizeToolName(strings.ToLower(method) + "_" + path)
}

// operationDescription returns a human-readable description for the operation.
func operationDescription(path, method string, op *openapi3.Operation) string {
	if op.Summary != "" {
		return op.Summary
	}

	if op.Description != "" {
		if len(op.Description) > 200 {
			return op.Description[:200] + "..."
		}
		return op.Description
	}

	return fmt.Sprintf("%s %s", method, path)
}

// operationSchema builds a JSON Schema object describing the tool parameters.
func operationSchema(op *openapi3.Operation) map[string]any {
	properties := map[string]any{}
	var required []string

	// Path and query parameters.
	for _, ref := range op.Parameters {
		if ref.Value == nil {
			continue
		}

		p := ref.Value
		prop := schemaRefToProperty(p.Schema)
		if p.Description != "" {
			prop["description"] = p.Description
		}

		properties[p.Name] = prop
		if p.Required {
			required = append(required, p.Name)
		}
	}

	// JSON request body properties (prefixed with "body_").
	if body := requestBodySchema(op); body != nil {
		for name, propRef := range body.Properties {
			properties["body_"+name] = schemaRefToProperty(propRef)
		}
		for _, req := range body.Required {
			required = append(required, "body_"+req)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// requestBodySchema extracts the JSON schema from an operation's request body, if any.
func requestBodySchema(op *openapi3.Operation) *openapi3.Schema {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}

	jsonContent, ok := op.RequestBody.Value.Content["application/json"]
	if !ok || jsonContent.Schema == nil || jsonContent.Schema.Value == nil {
		return nil
	}

	s := jsonContent.Schema.Value
	if s.Properties == nil {
		return nil
	}

	return s
}

// schemaRefToProperty converts an OpenAPI schema reference to a JSON Schema property map.
func schemaRefToProperty(ref *openapi3.SchemaRef) map[string]any {
	if ref == nil || ref.Value == nil {
		return map[string]any{"type": "string"}
	}

	s := ref.Value
	prop := map[string]any{
		"type": schemaType(s),
	}

	if s.Description != "" {
		prop["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		prop["enum"] = s.Enum
	}
	if s.Default != nil {
		prop["default"] = s.Default
	}

	return prop
}

// schemaType returns the JSON Schema type string for an OpenAPI schema.
// Defaults to "string" when the type is unspecified.
func schemaType(s *openapi3.Schema) string {
	if s.Type != nil {
		if types := s.Type.Slice(); len(types) > 0 {
			return types[0]
		}
	}

	return "string"
}

// sanitizeToolName converts a string into a valid tool name.
func sanitizeToolName(name string) string {
	name = strings.NewReplacer(
		"/", "_",
		"-", "_",
		".", "_",
		"{", "",
		"}", "",
	).Replace(name)

	name = strings.Trim(name, "_")

	// Collapse multiple underscores.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}

	return name
}

// setHeaders sets the User-Agent and custom headers on an HTTP request.
// Header values may contain ${headers.NAME} placeholders that are resolved
// from upstream headers stored in the request context.
func setHeaders(req *http.Request, headers map[string]string) {
	req.Header.Set("User-Agent", useragent.Header)
	for k, v := range upstream.ResolveHeaders(req.Context(), headers) {
		req.Header.Set(k, v)
	}
}

// openAPIHandler executes HTTP requests for an OpenAPI operation.
type openAPIHandler struct {
	baseURL string
	path    string
	method  string
	headers map[string]string
}

type openAPICallArgs map[string]any

func (h *openAPIHandler) callTool(ctx context.Context, params openAPICallArgs) (*tools.ToolCallResult, error) {
	resolvedPath, queryParams, bodyParams := h.classifyParams(params)

	fullURL := h.baseURL + resolvedPath
	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	var reqBody io.Reader = http.NoBody
	if len(bodyParams) > 0 {
		data, err := json.Marshal(bodyParams)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, h.method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if len(bodyParams) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	setHeaders(req, h.headers)

	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	output := limitOutput(string(body))
	if len(body) >= 1<<20 {
		output = "[WARNING: Response truncated at 1MB limit]\n" + output
	}

	if resp.StatusCode >= 400 {
		return tools.ResultError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, output)), nil
	}

	return tools.ResultSuccess(output), nil
}

// classifyParams splits tool call arguments into path replacements, query parameters,
// and body parameters based on the OpenAPI path template and "body_" prefix convention.
func (h *openAPIHandler) classifyParams(params openAPICallArgs) (string, url.Values, map[string]any) {
	resolvedPath := h.path
	queryParams := url.Values{}
	bodyParams := map[string]any{}

	for key, value := range params {
		// Path parameter?
		placeholder := "{" + key + "}"
		if strings.Contains(h.path, placeholder) {
			resolvedPath = strings.ReplaceAll(resolvedPath, placeholder, url.PathEscape(fmt.Sprintf("%v", value)))
			continue
		}

		// Body parameter? (prefixed with "body_")
		if strings.HasPrefix(key, "body_") {
			bodyParams[strings.TrimPrefix(key, "body_")] = value
			continue
		}

		// Otherwise it's a query parameter.
		queryParams.Set(key, fmt.Sprintf("%v", value))
	}

	return resolvedPath, queryParams, bodyParams
}
