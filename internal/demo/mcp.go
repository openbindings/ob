package demo

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewMCPHandler creates an http.Handler serving MCP tools via Streamable HTTP.
func NewMCPHandler(store *Store) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "openblendings",
		Version: "1.0.0",
	}, nil)

	type emptyArgs struct{}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "getMenu",
		Description: "Get the OpenBlendings menu with drinks, sizes, and prices",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args emptyArgs) (*mcp.CallToolResult, any, error) {
		return jsonResult(GetMenu()), nil, nil
	})

	type placeOrderArgs struct {
		Drink    string `json:"drink" jsonschema:"The name of the drink to order"`
		Size     string `json:"size" jsonschema:"Size: v1 (small), v2 (medium), or v3 (large)"`
		Customer string `json:"customer" jsonschema:"Customer name for the order"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "placeOrder",
		Description: "Place a coffee order at Blend",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args placeOrderArgs) (*mcp.CallToolResult, any, error) {
		output, err := PlaceOrder(store, PlaceOrderInput{
			Drink:    args.Drink,
			Size:     args.Size,
			Customer: args.Customer,
		})
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		return jsonResult(output), nil, nil
	})

	type getOrderStatusArgs struct {
		OrderID string `json:"orderId" jsonschema:"The order ID to check"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "getOrderStatus",
		Description: "Check the status of an order",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getOrderStatusArgs) (*mcp.CallToolResult, any, error) {
		output, err := GetOrderStatus(store, args.OrderID)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		return jsonResult(output), nil, nil
	})

	type cancelOrderArgs struct {
		OrderID string `json:"orderId" jsonschema:"The order ID to cancel"`
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "cancelOrder",
		Description: "Cancel a pending or preparing order",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args cancelOrderArgs) (*mcp.CallToolResult, any, error) {
		output, err := CancelOrder(store, args.OrderID)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil, nil
		}
		return jsonResult(output), nil, nil
	})

	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return server }, nil,
	)
}

func jsonResult(v any) *mcp.CallToolResult {
	data, _ := json.Marshal(v)
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}
}
