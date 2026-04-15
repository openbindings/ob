// Package internal contains the shared operation implementations for the Blend demo server.
// Operations delegate to the Store for stateful work and to the Menu for static data.
package demo

import (
	"fmt"
	"math"
)

// MenuResponse is the output of the getMenu operation.
type MenuResponse struct {
	Items []MenuItemWithPricing `json:"items"`
}

// MenuItemWithPricing extends MenuItem with size-specific pricing.
type MenuItemWithPricing struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Category    string      `json:"category"`
	Sizes       []SizePrice `json:"sizes"`
}

// SizePrice pairs a size with its price for a specific drink.
type SizePrice struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Price float64 `json:"price"`
}

// GetMenu returns the full menu with pricing.
func GetMenu() MenuResponse {
	var items []MenuItemWithPricing
	for _, drink := range Menu {
		item := MenuItemWithPricing{
			Name:        drink.Name,
			Description: drink.Description,
			Category:    drink.Category,
		}
		for _, sz := range Sizes {
			item.Sizes = append(item.Sizes, SizePrice{
				ID:    sz.ID,
				Label: sz.Label,
				Price: roundPrice(drink.BasePrice * sz.Mult),
			})
		}
		items = append(items, item)
	}
	return MenuResponse{Items: items}
}

// PlaceOrderInput is the input for the placeOrder operation.
type PlaceOrderInput struct {
	Drink    string `json:"drink"`
	Size     string `json:"size"`
	Customer string `json:"customer"`
}

// PlaceOrderOutput is the output of the placeOrder operation.
type PlaceOrderOutput struct {
	OrderID  string      `json:"orderId"`
	Status   OrderStatus `json:"status"`
	Drink    string      `json:"drink"`
	Size     string      `json:"size"`
	Customer string      `json:"customer"`
}

// PlaceOrder validates input and creates a new order.
func PlaceOrder(store *Store, input PlaceOrderInput) (PlaceOrderOutput, error) {
	if input.Customer == "" {
		return PlaceOrderOutput{}, fmt.Errorf("customer name is required")
	}
	if FindDrink(input.Drink) == nil {
		return PlaceOrderOutput{}, fmt.Errorf("unknown drink %q", input.Drink)
	}
	if FindSize(input.Size) == nil {
		return PlaceOrderOutput{}, fmt.Errorf("unknown size %q (use v1, v2, or v3)", input.Size)
	}

	order := store.Place(input.Drink, input.Size, input.Customer)
	return PlaceOrderOutput{
		OrderID:  order.ID,
		Status:   order.Status,
		Drink:    order.Drink,
		Size:     order.Size,
		Customer: order.Customer,
	}, nil
}

// GetOrderStatusOutput is the output of the getOrderStatus operation.
type GetOrderStatusOutput struct {
	OrderID   string      `json:"orderId"`
	Status    OrderStatus `json:"status"`
	Drink     string      `json:"drink"`
	Size      string      `json:"size"`
	Customer  string      `json:"customer"`
	CreatedAt string      `json:"createdAt"`
	UpdatedAt string      `json:"updatedAt"`
}

// GetOrderStatus retrieves an order's current status.
func GetOrderStatus(store *Store, orderID string) (GetOrderStatusOutput, error) {
	order := store.Get(orderID)
	if order == nil {
		return GetOrderStatusOutput{}, fmt.Errorf("order %q not found", orderID)
	}
	return GetOrderStatusOutput{
		OrderID:   order.ID,
		Status:    order.Status,
		Drink:     order.Drink,
		Size:      order.Size,
		Customer:  order.Customer,
		CreatedAt: order.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: order.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

// CancelOrderOutput is the output of the cancelOrder operation.
type CancelOrderOutput struct {
	OrderID string      `json:"orderId"`
	Status  OrderStatus `json:"status"`
}

// CancelOrder cancels a pending or preparing order.
func CancelOrder(store *Store, orderID string) (CancelOrderOutput, error) {
	order, ok := store.Cancel(orderID)
	if order == nil {
		return CancelOrderOutput{}, fmt.Errorf("order %q not found", orderID)
	}
	if !ok {
		return CancelOrderOutput{}, fmt.Errorf("order %q cannot be cancelled (status: %s)", orderID, order.Status)
	}
	return CancelOrderOutput{
		OrderID: order.ID,
		Status:  order.Status,
	}, nil
}

func roundPrice(f float64) float64 {
	return math.Round(f*100) / 100
}
