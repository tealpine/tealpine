package main

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"log"
	"os"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "client" {
		cli()
		return
	}
	// Create a new MCP server
	s := server.NewMCPServer(
		"Calculator Demo",
		"1.0.0",
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)

	// Add a calculator tool
	calculatorTool := mcp.NewTool("calculate",
		mcp.WithDescription("Perform basic arithmetic operations"),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Description("The operation to perform (add, subtract, multiply, divide)"),
			mcp.Enum("add", "subtract", "multiply", "divide"),
		),
		mcp.WithNumber("x",
			mcp.Required(),
			mcp.Description("First number"),
		),
		mcp.WithNumber("y",
			mcp.Required(),
			mcp.Description("Second number"),
		),
	)

	// Add the calculator handler
	s.AddTool(calculatorTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Using helper functions for type-safe argument access
		op, err := request.RequireString("operation")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		x, err := request.RequireFloat("x")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		y, err := request.RequireFloat("y")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var result float64
		switch op {
		case "add":
			result = x + y
		case "subtract":
			result = x - y
		case "multiply":
			result = x * y
		case "divide":
			if y == 0 {
				return mcp.NewToolResultError("cannot divide by zero"), nil
			}
			result = x / y
		}

		return mcp.NewToolResultText(fmt.Sprintf("%.2f", result)), nil
	})

	sseServer := server.NewSSEServer(s)

	// Start the server
	log.Println("Starting calculator MCP SSE server on port 7751")
	if err := sseServer.Start("localhost:7751"); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

func cli() {
	// Create a new SSE client
	c, err := client.NewSSEMCPClient("http://localhost:7751/sse")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		log.Fatalf("Failed to start transport: %v", err)
	}

	// Initialize the connection
	initResult, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "calculator-client",
				Version: "1.0.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	fmt.Printf("Connected to: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Println()

	// List available tools
	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		log.Fatalf("Failed to list tools: %v", err)
	}

	fmt.Println("Available tools:")
	for _, tool := range toolsResult.Tools {
		fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
	}
	fmt.Println()

	// Example calculations
	calculations := []struct {
		operation string
		x         float64
		y         float64
	}{
		{"add", 10, 5},
		{"subtract", 20, 8},
		{"multiply", 6, 7},
		{"divide", 100, 4},
		{"divide", 10, 0}, // This will trigger an error
	}

	for _, calc := range calculations {
		fmt.Printf("Calculating: %.2f %s %.2f\n", calc.x, calc.operation, calc.y)

		result, err := c.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "calculate",
				Arguments: map[string]interface{}{
					"operation": calc.operation,
					"x":         calc.x,
					"y":         calc.y,
				},
			},
		})

		if err != nil {
			log.Printf("Error calling tool: %v\n", err)
			continue
		}

		// Check if the result is an error
		if result.IsError {
			fmt.Printf("  Error: %s\n", result.Content[0])
		} else {
			fmt.Printf("  Result: %s\n", result.Content[0])
		}
		fmt.Println()
	}

	// Close the connection
	if err := c.Close(); err != nil {
		log.Printf("Error closing client: %v", err)
	}

	fmt.Println("Client finished successfully")
}
