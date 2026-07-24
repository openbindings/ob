package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/openbindings/ob/internal/app"
	"github.com/openbindings/ob/internal/demo"
)

const defaultDemoPort = 8080
const defaultDemoGRPCPort = 9090

func newDemoCmd() *cobra.Command {
	var (
		port     int
		grpcPort int
	)

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Start the OpenBlendings demo server (one interface, six protocols)",
		Long: `Start a local demo server that showcases OpenBindings in action.

The OpenBlendings coffee shop exposes six operations (getMenu, placeOrder,
getOrderStatus, cancelOrder, orderUpdates, placeAndTrack) across six
binding specifications simultaneously: REST (OpenAPI), gRPC, Connect, MCP,
GraphQL, and SSE (AsyncAPI). placeAndTrack is bound to an operation graph
that composes placeOrder with the orderUpdates stream.

One interface. Six protocols. Same operations.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseUnhonoredOutputFlags(cmd, "demo", "output", "format"); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			printBanner(port, grpcPort)

			// When the context is cancelled (first Ctrl+C), restore default
			// signal handling so a second Ctrl+C terminates immediately.
			go func() {
				<-ctx.Done()
				stop()
			}()

			err := demo.Start(ctx, demo.Config{
				Port:     port,
				GRPCPort: grpcPort,
			})
			if err != nil {
				return app.ExitResult{Code: 1, Message: err.Error(), ToStderr: true}
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", defaultDemoPort, "HTTP server port")
	cmd.Flags().IntVar(&grpcPort, "grpc-port", defaultDemoGRPCPort, "gRPC server port")

	return cmd
}

func printBanner(port, grpcPort int) {
	green := lipgloss.Color("2")
	cyan := lipgloss.Color("6")
	dim := lipgloss.Color("8")
	white := lipgloss.Color("15")

	s := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

	base := fmt.Sprintf("http://localhost:%d", port)
	w := os.Stderr
	arrow := s(green).Render(">")

	// Title
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s  %s\n",
		lipgloss.NewStyle().Bold(true).Foreground(white).Render("OpenBlendings"),
		s(dim).Render("one interface / six protocols / same operations"))
	fmt.Fprintln(w)

	// Endpoints
	ep := func(name, u string) {
		fmt.Fprintf(w, "  %s %-10s %s\n", arrow, s(dim).Render(name), s(cyan).Render(u))
	}
	ep("OBI", base+"/.well-known/openbindings")
	ep("REST", base+"/api/menu")
	ep("MCP", base+"/mcp")
	ep("GraphQL", base+"/graphql")
	ep("Connect", base+"/blend.CoffeeShop/GetMenu")
	ep("gRPC", fmt.Sprintf("localhost:%d", grpcPort))
	ep("SSE", base+"/events/orders")
	ep("OpenAPI", base+"/openapi.json")
	ep("AsyncAPI", base+"/asyncapi.json")
	fmt.Fprintln(w)

	// Discover
	h := lipgloss.NewStyle().Bold(true).Foreground(green)
	fmt.Fprintf(w, "  %s\n\n", h.Render("Discover"))
	fmt.Fprintf(w, "  $ ob resolve localhost:%d\n", port)
	fmt.Fprintf(w, "  $ ob validate %s\n", base)
	fmt.Fprintf(w, "  $ ob op list %s\n", base)
	fmt.Fprintln(w)

	// Invoke
	fmt.Fprintf(w, "  %s\n\n", h.Render("Invoke"))
	fmt.Fprintf(w, "  %s\n\n", s(dim).Render("Operations with several bindings require an explicit choice:"))
	tag := func(name string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(green).Bold(true).Padding(0, 1).Render(name)
	}
	fmt.Fprintf(w, "  $ ob op invoke %s --binding getMenu.restApi         %s\n", base, tag("REST"))
	fmt.Fprintf(w, "  $ ob op invoke %s --binding getMenu.connectServer  %s\n", base, tag("Connect"))
	fmt.Fprintf(w, "  $ ob op invoke %s --binding getMenu.grpcServer     %s\n", base, tag("gRPC"))
	fmt.Fprintf(w, "  $ ob op invoke %s --binding getMenu.mcpServer      %s\n", base, tag("MCP"))
	fmt.Fprintf(w, "  $ ob op invoke %s --binding getMenu.graphqlServer \\\n", base)
	fmt.Fprintf(w, "      --configuration '{\"document\":\"query { getMenu { items { name description category sizes { id label price } } } }\"}'  %s\n", tag("GraphQL"))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n\n", s(dim).Render("Place an order and watch it progress:"))
	fmt.Fprintf(w, "  $ ob op invoke %s --binding placeOrder.restApi \\\n", base)
	fmt.Fprintf(w, "      --input '{\"drink\":\"Schema Latte\",\"size\":\"v2\",\"customer\":\"Alice\"}'\n")
	fmt.Fprintf(w, "  $ ob op invoke %s --binding orderUpdates.grpcServer\n", base)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n\n", s(dim).Render("Or run the composed operation graph with caller-owned inner binding choices:"))
	fmt.Fprintf(w, "  $ ob op invoke %s placeAndTrack \\\n", base)
	fmt.Fprintln(w, "      --select-binding placeOrder.restApi --select-binding orderUpdates.grpcServer \\")
	fmt.Fprintf(w, "      --input '{\"drink\":\"Schema Latte\",\"size\":\"v2\",\"customer\":\"Alice\"}'\n")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "  %s\n\n", s(dim).Italic(true).Render("press ctrl+c to stop"))
}
