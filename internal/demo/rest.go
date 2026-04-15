package demo

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RegisterRESTRoutes sets up the REST API endpoints for OpenBlendings.
func RegisterRESTRoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("GET /api/menu", handleGetMenu)
	mux.HandleFunc("POST /api/orders", handlePlaceOrder(store))
	mux.HandleFunc("GET /api/orders/{orderId}", handleGetOrderStatus(store))
	mux.HandleFunc("POST /api/orders/{orderId}/cancel", handleCancelOrder(store))
}

func handleGetMenu(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, GetMenu())
}

func handlePlaceOrder(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input PlaceOrderInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		output, err := PlaceOrder(store, input)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, output)
	}
}

func handleGetOrderStatus(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("orderId")
		if id == "" {
			id = extractPathParam(r.URL.Path, "/api/orders/")
		}
		output, err := GetOrderStatus(store, id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, err.Error())
			} else {
				writeError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, output)
	}
}

func handleCancelOrder(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("orderId")
		if id == "" {
			id = extractPathParam(r.URL.Path, "/api/orders/")
			id = strings.TrimSuffix(id, "/cancel")
		}
		output, err := CancelOrder(store, id)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, err.Error())
			} else {
				writeError(w, http.StatusConflict, err.Error())
			}
			return
		}
		writeJSON(w, output)
	}
}

func extractPathParam(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
