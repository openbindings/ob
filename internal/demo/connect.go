package demo

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RegisterConnectRoutes sets up the Connect (Buf) protocol endpoints.
// Connect uses HTTP POST to /{service}/{method} with JSON payloads
// and the Connect-Protocol-Version: 1 header.
func RegisterConnectRoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("POST /blend.CoffeeShop/GetMenu", connectGetMenu)
	mux.HandleFunc("POST /blend.CoffeeShop/PlaceOrder", connectPlaceOrder(store))
	mux.HandleFunc("POST /blend.CoffeeShop/GetOrderStatus", connectGetOrderStatus(store))
	mux.HandleFunc("POST /blend.CoffeeShop/CancelOrder", connectCancelOrder(store))
}

func connectGetMenu(w http.ResponseWriter, r *http.Request) {
	writeConnectJSON(w, GetMenu())
}

func connectPlaceOrder(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input PlaceOrderInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		output, err := PlaceOrder(store, input)
		if err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		writeConnectJSON(w, output)
	}
}

func connectGetOrderStatus(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input connectOrderIDInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		output, err := GetOrderStatus(store, input.OrderID())
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeConnectError(w, http.StatusNotFound, "not_found", err.Error())
			} else {
				writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			}
			return
		}
		writeConnectJSON(w, output)
	}
}

func connectCancelOrder(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input connectOrderIDInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeConnectError(w, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		output, err := CancelOrder(store, input.OrderID())
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeConnectError(w, http.StatusNotFound, "not_found", err.Error())
			} else {
				writeConnectError(w, http.StatusBadRequest, "failed_precondition", err.Error())
			}
			return
		}
		writeConnectJSON(w, output)
	}
}

// connectOrderIDInput accepts both camelCase (proto3 JSON default) and
// snake_case (proto original name) for the order ID field.
type connectOrderIDInput struct {
	Camel string `json:"orderId"`
	Snake string `json:"order_id"`
}

func (c connectOrderIDInput) OrderID() string {
	if c.Camel != "" {
		return c.Camel
	}
	return c.Snake
}

func writeConnectJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func writeConnectError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"code":    code,
		"message": message,
	})
}
