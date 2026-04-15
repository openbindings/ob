package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"nhooyr.io/websocket"
)

// RegisterGraphQLRoutes sets up the GraphQL HTTP endpoint.
func RegisterGraphQLRoutes(mux *http.ServeMux, store *Store) {
	handler := &graphqlHandler{store: store}
	mux.Handle("POST /graphql", handler)
	mux.Handle("GET /graphql", handler) // For WebSocket upgrade (subscriptions)
}

type graphqlHandler struct {
	store *Store
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func (h *graphqlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle WebSocket upgrade for subscriptions.
	if r.Header.Get("Upgrade") == "websocket" || r.Header.Get("Connection") == "Upgrade" {
		h.handleWebSocket(w, r)
		return
	}

	// Handle graphql-transport-ws WebSocket sub-protocol.
	if strings.Contains(r.Header.Get("Sec-WebSocket-Protocol"), "graphql-transport-ws") {
		h.handleWebSocket(w, r)
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req graphqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeGraphQLError(w, "invalid request body")
		return
	}

	if req.Query == "" {
		writeGraphQLError(w, "missing query")
		return
	}

	// Handle introspection.
	if strings.Contains(req.Query, "__schema") || strings.Contains(req.Query, "__type") {
		h.handleIntrospection(w, req)
		return
	}

	// Resolve and execute the query/mutation.
	data, err := h.execute(req)
	if err != nil {
		writeGraphQLError(w, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (h *graphqlHandler) execute(req graphqlRequest) (map[string]any, error) {
	query := req.Query
	vars := req.Variables

	switch {
	case containsField(query, "getMenu"):
		result := GetMenu()
		return map[string]any{"getMenu": result}, nil

	case containsField(query, "placeOrder"):
		input := PlaceOrderInput{
			Drink:    stringVar(vars, "drink"),
			Size:     stringVar(vars, "size"),
			Customer: stringVar(vars, "customer"),
		}
		result, err := PlaceOrder(h.store, input)
		if err != nil {
			return nil, err
		}
		return map[string]any{"placeOrder": result}, nil

	case containsField(query, "getOrderStatus"):
		orderID := stringVar(vars, "orderId")
		result, err := GetOrderStatus(h.store, orderID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"getOrderStatus": result}, nil

	case containsField(query, "cancelOrder"):
		orderID := stringVar(vars, "orderId")
		result, err := CancelOrder(h.store, orderID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"cancelOrder": result}, nil

	default:
		return nil, fmt.Errorf("unknown field in query")
	}
}

// handleWebSocket handles GraphQL subscriptions via the graphql-transport-ws protocol.
func (h *graphqlHandler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{"graphql-transport-ws"},
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx := r.Context()

	// Wait for connection_init.
	if err := expectWSMessage(ctx, conn, "connection_init"); err != nil {
		return
	}

	// Send connection_ack.
	writeWSJSON(ctx, conn, map[string]any{"type": "connection_ack"})

	// Wait for subscribe message.
	_, raw, err := conn.Read(ctx)
	if err != nil {
		return
	}
	var msg struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Payload struct {
			Query string `json:"query"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "subscribe" {
		return
	}

	if !containsField(msg.Payload.Query, "orderUpdates") {
		writeWSJSON(ctx, conn, map[string]any{
			"id":      msg.ID,
			"type":    "error",
			"payload": []map[string]any{{"message": "only orderUpdates subscription is supported"}},
		})
		return
	}

	subID, ch := h.store.Subscribe()
	defer h.store.Unsubscribe(subID)

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-ch:
			if !ok {
				writeWSJSON(ctx, conn, map[string]any{"id": msg.ID, "type": "complete"})
				return
			}
			writeWSJSON(ctx, conn, map[string]any{
				"id":   msg.ID,
				"type": "next",
				"payload": map[string]any{
					"data": map[string]any{"orderUpdates": update},
				},
			})
		}
	}
}

func expectWSMessage(ctx context.Context, conn *websocket.Conn, expectedType string) error {
	_, raw, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	if msg.Type != expectedType {
		return fmt.Errorf("expected %q, got %q", expectedType, msg.Type)
	}
	return nil
}

func writeWSJSON(ctx context.Context, conn *websocket.Conn, v any) {
	raw, _ := json.Marshal(v)
	conn.Write(ctx, websocket.MessageText, raw)
}

func containsField(query, fieldName string) bool {
	return strings.Contains(query, fieldName+"(") ||
		strings.Contains(query, fieldName+" ") ||
		strings.Contains(query, fieldName+"{") ||
		strings.Contains(query, fieldName+"\n") ||
		strings.HasSuffix(strings.TrimSpace(query), fieldName)
}

func stringVar(vars map[string]any, key string) string {
	if vars == nil {
		return ""
	}
	v, _ := vars[key].(string)
	return v
}

func writeGraphQLError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]any{{"message": message}},
	})
}

// handleIntrospection returns the GraphQL schema for introspection queries.
func (h *graphqlHandler) handleIntrospection(w http.ResponseWriter, req graphqlRequest) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"data": map[string]any{
			"__schema": blendGraphQLSchema(),
		},
	})
}

// blendGraphQLSchema returns the introspection schema for OpenBlendings.
var blendSchemaOnce sync.Once
var blendSchema map[string]any

func blendGraphQLSchema() map[string]any {
	blendSchemaOnce.Do(func() {
		blendSchema = map[string]any{
			"queryType":        map[string]any{"name": "Query"},
			"mutationType":     map[string]any{"name": "Mutation"},
			"subscriptionType": map[string]any{"name": "Subscription"},
			"directives":       []any{},
			"types":            blendGraphQLTypes(),
		}
	})
	return blendSchema
}

func blendGraphQLTypes() []any {
	return []any{
		// Root types
		objectType("Query", "", []any{
			fieldDef("getMenu", "Get the OpenBlendings menu.", nil, namedType("MenuResponse")),
		}),
		objectType("Mutation", "", []any{
			fieldDef("placeOrder", "Place a new coffee order.", []any{
				argDef("drink", nonNull(scalar("String"))),
				argDef("size", nonNull(scalar("String"))),
				argDef("customer", nonNull(scalar("String"))),
			}, namedType("PlaceOrderOutput")),
			fieldDef("getOrderStatus", "Check the current status of an order.", []any{
				argDef("orderId", nonNull(scalar("String"))),
			}, namedType("GetOrderStatusOutput")),
			fieldDef("cancelOrder", "Cancel a pending or preparing order.", []any{
				argDef("orderId", nonNull(scalar("String"))),
			}, namedType("CancelOrderOutput")),
		}),
		objectType("Subscription", "", []any{
			fieldDef("orderUpdates", "Real-time stream of order status changes.", nil, namedType("OrderUpdate")),
		}),

		// Data types
		objectType("MenuResponse", "", []any{
			fieldDef("items", "", nil, listOf(namedType("MenuItem"))),
		}),
		objectType("MenuItem", "", []any{
			fieldDef("name", "", nil, scalar("String")),
			fieldDef("description", "", nil, scalar("String")),
			fieldDef("category", "", nil, scalar("String")),
			fieldDef("sizes", "", nil, listOf(namedType("SizePrice"))),
		}),
		objectType("SizePrice", "", []any{
			fieldDef("id", "", nil, scalar("String")),
			fieldDef("label", "", nil, scalar("String")),
			fieldDef("price", "", nil, scalar("Float")),
		}),
		objectType("PlaceOrderOutput", "", []any{
			fieldDef("orderId", "", nil, scalar("String")),
			fieldDef("status", "", nil, scalar("String")),
			fieldDef("drink", "", nil, scalar("String")),
			fieldDef("size", "", nil, scalar("String")),
			fieldDef("customer", "", nil, scalar("String")),
		}),
		objectType("GetOrderStatusOutput", "", []any{
			fieldDef("orderId", "", nil, scalar("String")),
			fieldDef("status", "", nil, scalar("String")),
			fieldDef("drink", "", nil, scalar("String")),
			fieldDef("size", "", nil, scalar("String")),
			fieldDef("customer", "", nil, scalar("String")),
			fieldDef("createdAt", "", nil, scalar("String")),
			fieldDef("updatedAt", "", nil, scalar("String")),
		}),
		objectType("CancelOrderOutput", "", []any{
			fieldDef("orderId", "", nil, scalar("String")),
			fieldDef("status", "", nil, scalar("String")),
		}),
		objectType("OrderUpdate", "", []any{
			fieldDef("orderId", "", nil, scalar("String")),
			fieldDef("status", "", nil, scalar("String")),
			fieldDef("drink", "", nil, scalar("String")),
			fieldDef("customer", "", nil, scalar("String")),
			fieldDef("timestamp", "", nil, scalar("String")),
		}),

		// Scalars
		scalarType("String"),
		scalarType("Float"),
		scalarType("Int"),
		scalarType("Boolean"),
		scalarType("ID"),
	}
}

// Schema helper builders for introspection response.

func objectType(name, desc string, fields []any) map[string]any {
	t := map[string]any{
		"kind":          "OBJECT",
		"name":          name,
		"fields":        fields,
		"inputFields":   nil,
		"enumValues":    nil,
		"interfaces":    []any{},
		"possibleTypes": nil,
	}
	if desc != "" {
		t["description"] = desc
	}
	return t
}

func scalarType(name string) map[string]any {
	return map[string]any{
		"kind":          "SCALAR",
		"name":          name,
		"description":   nil,
		"fields":        nil,
		"inputFields":   nil,
		"enumValues":    nil,
		"interfaces":    nil,
		"possibleTypes": nil,
	}
}

func fieldDef(name, desc string, args []any, typ map[string]any) map[string]any {
	if args == nil {
		args = []any{}
	}
	f := map[string]any{
		"name":              name,
		"args":              args,
		"type":              typ,
		"isDeprecated":      false,
		"deprecationReason": nil,
	}
	if desc != "" {
		f["description"] = desc
	}
	return f
}

func argDef(name string, typ map[string]any) map[string]any {
	return map[string]any{
		"name":         name,
		"type":         typ,
		"defaultValue": nil,
	}
}

func scalar(name string) map[string]any {
	return map[string]any{"kind": "SCALAR", "name": name, "ofType": nil}
}

func namedType(name string) map[string]any {
	return map[string]any{"kind": "OBJECT", "name": name, "ofType": nil}
}

func nonNull(inner map[string]any) map[string]any {
	return map[string]any{"kind": "NON_NULL", "name": nil, "ofType": inner}
}

func listOf(inner map[string]any) map[string]any {
	return map[string]any{"kind": "LIST", "name": nil, "ofType": inner}
}
