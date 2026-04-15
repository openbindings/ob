package demo

import (
	"context"
	"fmt"
	"net"

	pb "github.com/openbindings/ob/internal/demo/proto/blend"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// BlendGRPCServer implements the CoffeeShop gRPC service.
type BlendGRPCServer struct {
	pb.UnimplementedCoffeeShopServer
	store *Store
}

func (s *BlendGRPCServer) GetMenu(ctx context.Context, req *pb.GetMenuRequest) (*pb.GetMenuResponse, error) {
	menu := GetMenu()
	resp := &pb.GetMenuResponse{}
	for _, item := range menu.Items {
		pbItem := &pb.MenuItemMessage{
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
		}
		for _, sz := range item.Sizes {
			pbItem.Sizes = append(pbItem.Sizes, &pb.SizePriceMessage{
				Id:    sz.ID,
				Label: sz.Label,
				Price: sz.Price,
			})
		}
		resp.Items = append(resp.Items, pbItem)
	}
	return resp, nil
}

func (s *BlendGRPCServer) PlaceOrder(ctx context.Context, req *pb.PlaceOrderRequest) (*pb.PlaceOrderResponse, error) {
	output, err := PlaceOrder(s.store, PlaceOrderInput{
		Drink:    req.Drink,
		Size:     req.Size,
		Customer: req.Customer,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.PlaceOrderResponse{
		OrderId:  output.OrderID,
		Status:   string(output.Status),
		Drink:    output.Drink,
		Size:     output.Size,
		Customer: output.Customer,
	}, nil
}

func (s *BlendGRPCServer) GetOrderStatus(ctx context.Context, req *pb.GetOrderStatusRequest) (*pb.GetOrderStatusResponse, error) {
	output, err := GetOrderStatus(s.store, req.OrderId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return &pb.GetOrderStatusResponse{
		OrderId:   output.OrderID,
		Status:    string(output.Status),
		Drink:     output.Drink,
		Size:      output.Size,
		Customer:  output.Customer,
		CreatedAt: output.CreatedAt,
		UpdatedAt: output.UpdatedAt,
	}, nil
}

func (s *BlendGRPCServer) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	output, err := CancelOrder(s.store, req.OrderId)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &pb.CancelOrderResponse{
		OrderId: output.OrderID,
		Status:  string(output.Status),
	}, nil
}

func (s *BlendGRPCServer) OrderUpdates(req *pb.OrderUpdatesRequest, stream grpc.ServerStreamingServer[pb.OrderUpdate]) error {
	subID, ch := s.store.Subscribe()
	defer s.store.Unsubscribe(subID)

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case update, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(&pb.OrderUpdate{
				OrderId:   update.OrderID,
				Status:    string(update.Status),
				Drink:     update.Drink,
				Customer:  update.Customer,
				Timestamp: update.Timestamp.Format("2006-01-02T15:04:05Z"),
			}); err != nil {
				return err
			}
		}
	}
}

// StartGRPCServer creates and starts a gRPC server on the given port.
// Returns the server so it can be stopped gracefully.
func StartGRPCServer(store *Store, port int) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("listen on :%d: %w", port, err)
	}

	srv := grpc.NewServer()
	pb.RegisterCoffeeShopServer(srv, &BlendGRPCServer{store: store})
	reflection.Register(srv)

	go func() {
		if err := srv.Serve(lis); err != nil {
			fmt.Printf("gRPC server error: %v\n", err)
		}
	}()

	return srv, nil
}
