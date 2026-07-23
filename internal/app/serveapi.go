package app

import (
	"fmt"
	"strings"

	openbindings "github.com/openbindings/openbindings-go"
	"gopkg.in/yaml.v3"
)

// ServeRoute is one canonical HTTP realization of an ob contract operation.
// The route table is the public API inventory: handlers and the generated
// OpenAPI document are tested against it so command/API coverage cannot drift.
type ServeRoute struct {
	Method       string
	Path         string
	Operation    string
	PathParam    string
	BodySchema   openbindings.JSONSchema
	Success      int
	ResponseType string
	Notes        string
}

// ServeHTTPRoutes returns the canonical, document-oriented HTTP surface.
// invokeBinding and invokeOperation are intentionally absent: their full
// cardinality rides the AsyncAPI WebSocket frame transport.
func ServeHTTPRoutes() []ServeRoute {
	return []ServeRoute{
		{Method: "get", Path: "/describe", Operation: "describe"},
		{Method: "get", Path: "/binding-specs", Operation: "listBindingSpecs"},
		{Method: "post", Path: "/interfaces/resolve", Operation: "resolveInterface"},
		{Method: "post", Path: "/interfaces/synthesize", Operation: "synthesizeInterface"},
		{Method: "post", Path: "/sources/inspect", Operation: "inspectSource"},
		{Method: "post", Path: "/interfaces/sources/add", Operation: "addSource"},
		{Method: "post", Path: "/interfaces/sources/remove", Operation: "removeSource"},
		{Method: "post", Path: "/interfaces/sources/list", Operation: "listSources"},
		{Method: "post", Path: "/interfaces/bindings/list", Operation: "listBindings"},
		{Method: "post", Path: "/interfaces/operations/add", Operation: "addOperation"},
		{Method: "post", Path: "/interfaces/operations/rename", Operation: "renameOperation"},
		{Method: "post", Path: "/interfaces/operations/remove", Operation: "removeOperation"},
		{Method: "post", Path: "/interfaces/operations/list", Operation: "listOperations"},
		{Method: "post", Path: "/interfaces/merge", Operation: "mergeInterfaces"},
		{Method: "post", Path: "/interfaces/validate", Operation: "validateInterface"},
		{Method: "post", Path: "/interfaces/compare", Operation: "compareInterfaces"},
		{Method: "post", Path: "/interfaces/compatibility", Operation: "reportCompatibility"},
		{Method: "post", Path: "/bindings/prepare", Operation: "prepareBinding"},
		// openbindings.openapi@1 flattens top-level request-object properties
		// and intentionally refuses a top-level conditional schema.
		// OperationInvocationInput has an operation-vs-binding oneOf, so the
		// HTTP artifact carries it under one transport-only `input` property;
		// the generated binding unwraps that property with an input transform.
		{Method: "post", Path: "/operations/prepare", Operation: "prepareOperation", BodySchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"$ref": "#/schemas/OperationInvocationInput"},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		}},
		{Method: "get", Path: "/contexts/{url}", Operation: "getContext", PathParam: "url"},
		{Method: "put", Path: "/contexts/{url}", Operation: "setContext", PathParam: "url", BodySchema: map[string]any{"$ref": "#/components/schemas/BindingContext"}, Success: 204},
		{Method: "delete", Path: "/contexts/{url}", Operation: "removeContext", PathParam: "url", Success: 204},
		{Method: "get", Path: "/contexts", Operation: "listContexts"},
		{Method: "post", Path: "/delegates/register", Operation: "registerDelegate"},
		{Method: "post", Path: "/delegates/unregister", Operation: "unregisterDelegate"},
		{Method: "get", Path: "/delegates", Operation: "listDelegates"},
		{Method: "get", Path: "/delegates/resolve/{operation}", Operation: "resolveDelegate", PathParam: "operation"},
		{Method: "post", Path: "/delegates/resolve-binding-spec", Operation: "resolveDelegateForBindingSpec"},
		{Method: "get", Path: "/delegate-requirements/{capability}", Operation: "getDelegateRequirements", PathParam: "capability"},
		{Method: "post", Path: "/delegates/preference", Operation: "setDelegatePreference"},
		{Method: "post", Path: "/environment/initialize", Operation: "initializeEnvironment"},
		{Method: "get", Path: "/environment", Operation: "reportEnvironmentStatus"},
		{Method: "post", Path: "/interfaces/status", Operation: "reportInterfaceStatus"},
		{Method: "post", Path: "/interfaces/codegen", Operation: "codegen"},
		{Method: "post", Path: "/interfaces/conform", Operation: "conform"},
		{Method: "post", Path: "/interfaces/operations/aliases/add", Operation: "addOperationAlias"},
		{Method: "post", Path: "/interfaces/operations/aliases/remove", Operation: "removeOperationAlias"},
		{Method: "post", Path: "/interfaces/operations/aliases/list", Operation: "listOperationAliases"},
		{Method: "post", Path: "/interfaces/sources/pull", Operation: "pullSource", Notes: "Inline interface documents have no originating directory. Tracked relative source references are rejected; embed the source or use an absolute reference before calling this endpoint."},
		{Method: "post", Path: "/interfaces/purify", Operation: "purifyInterface", ResponseType: "obi"},
		{Method: "post", Path: "/interfaces/operations/set", Operation: "setOperation"},
		{Method: "post", Path: "/interfaces/operations/detach", Operation: "detachOperation"},
		{Method: "post", Path: "/interfaces/operations/codegen-name", Operation: "setOperationCodegenName"},
		{Method: "post", Path: "/interfaces/operations/output-schema", Operation: "setOperationOutputSchema"},
		{Method: "post", Path: "/interfaces/operations/bind", Operation: "bindOperation"},
		{Method: "post", Path: "/interfaces/operations/unbind", Operation: "unbindOperation"},
		{Method: "post", Path: "/interfaces", Operation: "newInterface", Success: 201, ResponseType: "obi"},
		{Method: "post", Path: "/interfaces/metadata", Operation: "setMetadata"},
	}
}

// GenerateServeOpenAPI renders the canonical OpenAPI 3.1 source from ob's
// operation contract plus ServeHTTPRoutes. OBI JSON Schemas are already JSON
// Schema 2020-12, so they can be carried into OpenAPI 3.1 without weakening.
func GenerateServeOpenAPI(contractPath, serverURL string) ([]byte, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}

	paths := map[string]any{}
	seen := map[string]bool{}
	for _, route := range ServeHTTPRoutes() {
		opKey := "openbindings.ob." + route.Operation
		op, ok := contract.Operations[opKey]
		if !ok {
			return nil, fmt.Errorf("serve route %s %s references unknown operation %s", route.Method, route.Path, opKey)
		}
		if seen[opKey] {
			return nil, fmt.Errorf("operation %s has more than one canonical HTTP route", opKey)
		}
		seen[opKey] = true

		responses := map[string]any{
			fmt.Sprintf("%d", successStatus(route)): successResponse(op.Output, route.ResponseType, successStatus(route)),
			"400":                                   errorResponse("The request is invalid or the operation cannot be applied."),
			"401":                                   errorResponse("A valid bearer or OAuth access token is required."),
			"413":                                   errorResponse("The request body exceeds the server's delivery-unit limit."),
			"415":                                   errorResponse("The request body is not JSON."),
			"default":                               errorResponse("The operation failed."),
		}
		description := op.Description
		if route.Notes != "" {
			description += "\n\n" + route.Notes
		}
		operation := map[string]any{
			"operationId": route.Operation,
			"summary":     firstSentence(op.Description),
			"description": description,
			"responses":   responses,
		}
		if route.PathParam != "" {
			operation["parameters"] = []any{map[string]any{
				"name": route.PathParam, "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			}}
		}
		if route.Method != "get" && route.Method != "delete" {
			schema := route.BodySchema
			if schema == nil {
				schema = op.Input
			}
			if schema == nil {
				schema = map[string]any{"type": "object", "additionalProperties": false}
			}
			operation["requestBody"] = map[string]any{
				"required": true,
				"content":  map[string]any{"application/json": map[string]any{"schema": openAPISchema(schema)}},
			}
		}

		pathItem, _ := paths[route.Path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[route.Path] = pathItem
		}
		pathItem[route.Method] = operation
	}

	addInfrastructurePaths(paths)

	schemas := map[string]any{}
	for name, schema := range contract.Schemas {
		schemas[name] = openAPISchema(schema)
	}
	schemas["ErrorResponse"] = map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"error"},
		"properties": map[string]any{
			"error":  map[string]any{"type": "string", "description": "Human-readable error message."},
			"code":   map[string]any{"type": "string", "description": "Stable machine-readable error code."},
			"detail": map[string]any{"type": "string", "description": "Optional diagnostic detail."},
		},
	}

	doc := struct {
		OpenAPI    string         `yaml:"openapi"`
		Info       map[string]any `yaml:"info"`
		Servers    []any          `yaml:"servers"`
		Paths      map[string]any `yaml:"paths"`
		Components map[string]any `yaml:"components"`
		Security   []any          `yaml:"security"`
	}{
		OpenAPI: "3.1.0",
		Info: map[string]any{
			"title": "ob start API", "version": contract.Version,
			"description": "A local, document-oriented API for inspecting, authoring, invoking, and operating OpenBindings interfaces. JSON request bodies and individual WebSocket frames are limited to 10 MiB.",
		},
		Servers: []any{map[string]any{"url": serverURL, "description": "This ob start instance."}},
		Paths:   paths,
		Components: map[string]any{
			"securitySchemes": map[string]any{
				"oauth2Auth": map[string]any{
					"type": "oauth2",
					"flows": map[string]any{"authorizationCode": map[string]any{
						"authorizationUrl": serverURL + "/oauth/authorize", "tokenUrl": serverURL + "/oauth/token", "scopes": map[string]any{},
					}},
				},
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "bearerFormat": "opaque"},
			},
			"schemas": schemas,
		},
		Security: []any{map[string]any{"oauth2Auth": []any{}}, map[string]any{"bearerAuth": []any{}}},
	}
	return yaml.Marshal(doc)
}

// GenerateServeAsyncAPI renders the two WebSocket frame operations from the
// same contract schemas as the HTTP API.
func GenerateServeAsyncAPI(contractPath, serverHost, serverProtocol string) ([]byte, error) {
	contract, err := loadInterfaceFile(contractPath)
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}
	schemas := map[string]any{}
	for name, schema := range contract.Schemas {
		schemas[name] = openAPISchema(schema)
	}

	channels := map[string]any{}
	operations := map[string]any{}
	messages := map[string]any{}
	for _, inv := range []struct {
		Short        string
		Channel      string
		Address      string
		InputSchema  string
		OutputSchema string
	}{
		{Short: "invokeBinding", Channel: "bindingsInvoke", Address: "/bindings/invoke", InputSchema: "BindingInvokerInputFrame", OutputSchema: "BindingInvokerOutputFrame"},
		{Short: "invokeOperation", Channel: "operationsInvoke", Address: "/operations/invoke", InputSchema: "OperationInvokerInputFrame", OutputSchema: "OperationInvokerOutputFrame"},
	} {
		inputMessage := inv.Short + "InputFrame"
		outputMessage := inv.Short + "OutputFrame"
		messages[inputMessage] = map[string]any{
			"name": inputMessage, "payload": map[string]any{"$ref": "#/components/schemas/" + inv.InputSchema},
		}
		messages[outputMessage] = map[string]any{
			"name": outputMessage, "payload": map[string]any{"$ref": "#/components/schemas/" + inv.OutputSchema},
		}
		channels[inv.Channel] = map[string]any{
			"address":  inv.Address,
			"bindings": map[string]any{"ws": map[string]any{"method": "GET"}},
			"messages": map[string]any{
				"inputFrame":  map[string]any{"$ref": "#/components/messages/" + inputMessage},
				"outputFrame": map[string]any{"$ref": "#/components/messages/" + outputMessage},
			},
		}
		op := contract.Operations["openbindings.ob."+inv.Short]
		operations[inv.Short] = map[string]any{
			"action": "receive", "summary": firstSentence(op.Description), "description": op.Description,
			"channel":  map[string]any{"$ref": "#/channels/" + inv.Channel},
			"messages": []any{map[string]any{"$ref": "#/channels/" + inv.Channel + "/messages/inputFrame"}},
			"reply": map[string]any{
				"channel":  map[string]any{"$ref": "#/channels/" + inv.Channel},
				"messages": []any{map[string]any{"$ref": "#/channels/" + inv.Channel + "/messages/outputFrame"}},
			},
		}
	}

	doc := struct {
		AsyncAPI   string         `yaml:"asyncapi"`
		Info       map[string]any `yaml:"info"`
		Servers    map[string]any `yaml:"servers"`
		Channels   map[string]any `yaml:"channels"`
		Operations map[string]any `yaml:"operations"`
		Components map[string]any `yaml:"components"`
	}{
		AsyncAPI: "3.0.0",
		Info: map[string]any{
			"title": "ob start invocation API", "version": contract.Version,
			"description": "Cardinality-agnostic binding and operation invocation over WebSocket frame streams.",
		},
		Servers: map[string]any{"local": map[string]any{
			"host": serverHost, "protocol": serverProtocol,
			"security": []any{
				map[string]any{"$ref": "#/components/securitySchemes/bearer"},
				map[string]any{"$ref": "#/components/securitySchemes/tokenQuery"},
			},
		}},
		Channels:   channels,
		Operations: operations,
		Components: map[string]any{
			"messages": messages,
			"securitySchemes": map[string]any{
				"bearer": map[string]any{
					"type":        "httpBearer",
					"description": "Session or OAuth access token in the Authorization header on the WebSocket upgrade request.",
				},
				"tokenQuery": map[string]any{
					"type":        "apiKey",
					"in":          "query",
					"name":        "token",
					"description": "Session or OAuth access token for browser WebSocket clients, which cannot set upgrade headers.",
				},
			},
			"schemas": schemas,
		},
	}
	return yaml.Marshal(doc)
}

func successStatus(route ServeRoute) int {
	if route.Success != 0 {
		return route.Success
	}
	return 200
}

func successResponse(output openbindings.JSONSchema, responseType string, status int) map[string]any {
	if status == 204 {
		return map[string]any{"description": "The operation completed successfully; there is no response body."}
	}
	schema := output
	if schema == nil {
		schema = map[string]any{"type": "null"}
	}
	media := "application/json"
	if responseType == "obi" || schemaRef(schema) == "#/schemas/OpenBindingsInterface" {
		media = "application/vnd.openbindings+json"
	}
	return map[string]any{
		"description": "Successful response.",
		"content":     map[string]any{media: map[string]any{"schema": openAPISchema(schema)}},
	}
}

func schemaRef(schema any) string {
	if m, ok := schema.(map[string]any); ok {
		if ref, ok := m["$ref"].(string); ok {
			return ref
		}
	}
	return ""
}

func errorResponse(description string) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
		}},
	}
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	return s
}

func openAPISchema(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, value := range x {
			if k == "$ref" {
				if ref, ok := value.(string); ok {
					value = strings.Replace(ref, "#/schemas/", "#/components/schemas/", 1)
				}
			}
			out[k] = openAPISchema(value)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = openAPISchema(x[i])
		}
		return out
	default:
		return v
	}
}

func addInfrastructurePaths(paths map[string]any) {
	paths["/healthz"] = map[string]any{"get": publicGet("healthCheck", "Report whether the server is ready.", "application/json", map[string]any{
		"type": "object", "required": []string{"status"}, "properties": map[string]any{"status": map[string]any{"type": "string", "const": "ok"}},
	})}
	paths["/.well-known/openbindings"] = map[string]any{"get": publicGet("getOBI", "Return the interface implemented by this server.", "application/vnd.openbindings+json", map[string]any{"$ref": "#/components/schemas/OpenBindingsInterface"})}
	paths["/openapi.yaml"] = map[string]any{"get": publicGet("getOpenAPISpec", "Return this OpenAPI document.", "text/yaml", map[string]any{"type": "string"})}
	paths["/asyncapi.yaml"] = map[string]any{"get": publicGet("getAsyncAPISpec", "Return the WebSocket frame API document.", "text/yaml", map[string]any{"type": "string"})}
	paths["/spec/{name}"] = map[string]any{"get": map[string]any{
		"operationId": "readSpecResource", "summary": "Read a bundled specification resource.",
		"parameters": []any{map[string]any{"name": "name", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
		"responses":  map[string]any{"200": map[string]any{"description": "Resource contents.", "content": map[string]any{"text/markdown": map[string]any{"schema": map[string]any{"type": "string"}}}}},
	}}
	paths["/oauth/authorize"] = map[string]any{
		"get": map[string]any{
			"operationId": "oauthAuthorize", "summary": "Begin OAuth authorization with PKCE.", "security": []any{},
			"parameters": []any{
				queryParameter("response_type", true, map[string]any{"type": "string", "const": "code"}),
				queryParameter("client_id", true, map[string]any{"type": "string"}),
				queryParameter("redirect_uri", true, map[string]any{"type": "string", "format": "uri"}),
				queryParameter("code_challenge", true, map[string]any{"type": "string"}),
				queryParameter("code_challenge_method", true, map[string]any{"type": "string", "const": "S256"}),
				queryParameter("state", false, map[string]any{"type": "string"}),
			},
			"responses": map[string]any{"200": map[string]any{
				"description": "HTML consent page.",
				"content":     map[string]any{"text/html": map[string]any{"schema": map[string]any{"type": "string"}}},
			}},
		},
		"post": map[string]any{
			"operationId": "oauthApprove", "summary": "Submit an OAuth consent decision.", "security": []any{},
			"requestBody": formRequest(map[string]any{
				"type": "object", "additionalProperties": false, "required": []string{"nonce", "action"},
				"properties": map[string]any{
					"nonce":  map[string]any{"type": "string"},
					"action": map[string]any{"type": "string", "enum": []string{"approve", "deny"}},
				},
			}),
			"responses": map[string]any{"302": map[string]any{"description": "Redirect to the client's redirect URI."}},
		},
	}
	paths["/oauth/token"] = map[string]any{"post": map[string]any{
		"operationId": "oauthToken", "summary": "Exchange an authorization code for an access token.", "security": []any{},
		"requestBody": formRequest(map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"grant_type", "code", "code_verifier"},
			"properties": map[string]any{
				"grant_type":    map[string]any{"type": "string", "const": "authorization_code"},
				"code":          map[string]any{"type": "string"},
				"code_verifier": map[string]any{"type": "string"},
				"redirect_uri":  map[string]any{"type": "string", "format": "uri"},
			},
		}),
		"responses": map[string]any{"200": map[string]any{
			"description": "Access token.",
			"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"access_token", "token_type", "expires_in"},
				"properties": map[string]any{
					"access_token": map[string]any{"type": "string"},
					"token_type":   map[string]any{"type": "string", "const": "Bearer"},
					"expires_in":   map[string]any{"type": "integer", "minimum": 0},
				},
			}}},
		}},
	}}
}

func queryParameter(name string, required bool, schema map[string]any) map[string]any {
	return map[string]any{"name": name, "in": "query", "required": required, "schema": schema}
}

func formRequest(schema map[string]any) map[string]any {
	return map[string]any{
		"required": true,
		"content":  map[string]any{"application/x-www-form-urlencoded": map[string]any{"schema": schema}},
	}
}

func publicGet(operationID, summary, mediaType string, schema any) map[string]any {
	return map[string]any{
		"operationId": operationID, "summary": summary, "security": []any{},
		"responses": map[string]any{"200": map[string]any{
			"description": "Successful response.", "content": map[string]any{mediaType: map[string]any{"schema": schema}},
		}},
	}
}
