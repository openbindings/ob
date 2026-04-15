package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterSSERoutes sets up the SSE event endpoints.
func RegisterSSERoutes(mux *http.ServeMux, store *Store) {
	mux.HandleFunc("GET /events/orders", handleSSEOrders(store))
}

func handleSSEOrders(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		subID, ch := store.Subscribe()
		defer store.Unsubscribe(subID)

		for {
			select {
			case <-r.Context().Done():
				return
			case update, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(update)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}
