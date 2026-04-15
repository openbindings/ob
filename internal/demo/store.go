package demo

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	StatusReceived  OrderStatus = "received"
	StatusPreparing OrderStatus = "preparing"
	StatusReady     OrderStatus = "ready"
	StatusPickedUp  OrderStatus = "picked_up"
	StatusCancelled OrderStatus = "cancelled"
)

// Order represents a coffee order.
type Order struct {
	ID        string      `json:"id"`
	Drink     string      `json:"drink"`
	Size      string      `json:"size"`
	Customer  string      `json:"customer"`
	Status    OrderStatus `json:"status"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// OrderUpdate is emitted when an order's status changes.
type OrderUpdate struct {
	OrderID   string      `json:"orderId"`
	Status    OrderStatus `json:"status"`
	Drink     string      `json:"drink"`
	Customer  string      `json:"customer"`
	Timestamp time.Time   `json:"timestamp"`
}

// Store is a thread-safe in-memory order store with automatic state progression.
type Store struct {
	mu        sync.RWMutex
	orders    map[string]*Order
	listeners map[int]chan OrderUpdate
	nextID    int
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewStore creates a store and starts the background goroutines for
// order progression and expiry.
func NewStore() *Store {
	s := &Store{
		orders:    make(map[string]*Order),
		listeners: make(map[int]chan OrderUpdate),
		stopCh:    make(chan struct{}),
	}
	go s.progressLoop()
	go s.expiryLoop()
	return s
}

// Stop shuts down background goroutines. Safe to call multiple times.
func (s *Store) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Place creates a new order and returns it.
func (s *Store) Place(drink, size, customer string) *Order {
	now := time.Now().UTC()
	order := &Order{
		ID:        generateID(),
		Drink:     drink,
		Size:      size,
		Customer:  customer,
		Status:    StatusReceived,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	s.orders[order.ID] = order
	s.mu.Unlock()

	s.broadcast(OrderUpdate{
		OrderID:   order.ID,
		Status:    order.Status,
		Drink:     order.Drink,
		Customer:  order.Customer,
		Timestamp: now,
	})

	return order
}

// Get returns an order by ID, or nil if not found.
func (s *Store) Get(id string) *Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o := s.orders[id]
	if o == nil {
		return nil
	}
	cp := *o
	return &cp
}

// Cancel cancels a pending order. Returns false if the order doesn't exist
// or is already past the preparing stage.
func (s *Store) Cancel(id string) (*Order, bool) {
	s.mu.Lock()
	o := s.orders[id]
	if o == nil {
		s.mu.Unlock()
		return nil, false
	}
	if o.Status != StatusReceived && o.Status != StatusPreparing {
		cp := *o
		s.mu.Unlock()
		return &cp, false
	}
	now := time.Now().UTC()
	o.Status = StatusCancelled
	o.UpdatedAt = now
	cp := *o
	s.mu.Unlock()

	s.broadcast(OrderUpdate{
		OrderID:   o.ID,
		Status:    StatusCancelled,
		Drink:     o.Drink,
		Customer:  o.Customer,
		Timestamp: now,
	})

	return &cp, true
}

// Subscribe returns a channel that receives order updates.
// Call Unsubscribe with the returned ID when done.
func (s *Store) Subscribe() (int, <-chan OrderUpdate) {
	ch := make(chan OrderUpdate, 32)
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = ch
	s.mu.Unlock()
	return id, ch
}

// Unsubscribe removes a listener and closes its channel.
func (s *Store) Unsubscribe(id int) {
	s.mu.Lock()
	ch, ok := s.listeners[id]
	if ok {
		delete(s.listeners, id)
		close(ch)
	}
	s.mu.Unlock()
}

func (s *Store) broadcast(update OrderUpdate) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.listeners {
		select {
		case ch <- update:
		default:
		}
	}
}

// progressLoop advances orders through their lifecycle.
func (s *Store) progressLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.advanceOrders(now.UTC())
		}
	}
}

func (s *Store) advanceOrders(now time.Time) {
	s.mu.Lock()
	var updates []OrderUpdate
	for _, o := range s.orders {
		var next OrderStatus
		switch o.Status {
		case StatusReceived:
			if now.Sub(o.UpdatedAt) >= 5*time.Second {
				next = StatusPreparing
			}
		case StatusPreparing:
			if now.Sub(o.UpdatedAt) >= 5*time.Second {
				next = StatusReady
			}
		case StatusReady:
			if now.Sub(o.UpdatedAt) >= 10*time.Second {
				next = StatusPickedUp
			}
		}
		if next != "" {
			o.Status = next
			o.UpdatedAt = now
			updates = append(updates, OrderUpdate{
				OrderID:   o.ID,
				Status:    next,
				Drink:     o.Drink,
				Customer:  o.Customer,
				Timestamp: now,
			})
		}
	}
	s.mu.Unlock()

	for _, u := range updates {
		s.broadcast(u)
	}
}

// expiryLoop removes orders older than 5 minutes.
func (s *Store) expiryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.mu.Lock()
			for id, o := range s.orders {
				if now.UTC().Sub(o.CreatedAt) > 5*time.Minute {
					delete(s.orders, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
