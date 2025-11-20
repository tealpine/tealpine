package test

import (
	"context"
	"fmt"
	mgclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

type MCPTestServer interface {
	RunServer() error
	RunClient(string) error
	GetHTTPHandler() (http.Handler, error)
}

type MCPCalculator struct {
}

var _ MCPTestServer = (*MCPCalculator)(nil)

func (m *MCPCalculator) GetHTTPHandler() (http.Handler, error) {
	s := m.createMCPServer()
	sseServer := server.NewSSEServer(s)
	return sseServer, nil
}

func (m *MCPCalculator) createMCPServer() *server.MCPServer {
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

	return s
}

func (m *MCPCalculator) RunServer() error {
	s := m.createMCPServer()
	sseServer := server.NewSSEServer(s)

	// Start the server
	log.Println("Starting calculator MCP SSE server on port 7751")
	if err := sseServer.Start("localhost:7751"); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPCalculator) RunClient(url string) error {
	// Create a new SSE client
	if url == "" {
		url = "http://localhost:7751/sse"
	}
	c, err := mgclient.NewSSEMCPClient(url)
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
		return err
	}

	fmt.Println("Client finished successfully")
	return nil
}

type MCPHello struct{}

var _ MCPTestServer = (*MCPHello)(nil)

func (m *MCPHello) GetHTTPHandler() (http.Handler, error) {
	return nil, fmt.Errorf("GetHTTPHandler not implemented for MCPHello")
}

func (m *MCPHello) RunServer() error {
	// Create a new MCP server
	s := server.NewMCPServer(
		"Demo 🚀",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	// Add tool
	tool := mcp.NewTool("hello_world",
		mcp.WithDescription("Say hello to someone"),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Name of the person to greet"),
		),
	)

	// Add tool handler
	s.AddTool(tool, helloHandler)

	// Start the stdio server
	if err := server.ServeStdio(s); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPHello) RunClient(string) error {
	fmt.Printf("%v\n", os.Args)
	client, err := mgclient.NewStdioMCPClient(os.Args[0], []string{}, "hello", "server")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	ctx := context.Background()
	result, err := client.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-auth-proxy-upstream-client",
				Version: "1.0.0",
			},
		},
	})
	fmt.Printf("RESULT: %v\n", result)
	return nil
}

func helloHandler(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}

type MCPTemperature struct{}

var _ MCPTestServer = (*MCPTemperature)(nil)

var rooms = []string{"kitchen", "bathroom", "living room", "bedroom", "kids room"}

// getRoomTemperature returns a random temperature between 20 and 24°C
func getRoomTemperature(room string) float64 {
	// Random number between 20.0 and 24.0
	temp := 20.0 + rand.Float64()*4.0
	// Round to 1 decimal place
	return float64(int(temp*10)) / 10
}

// isValidRoom checks if the room name is valid
func isValidRoom(room string) bool {
	for _, r := range rooms {
		if r == room {
			return true
		}
	}
	return false
}

func (m *MCPTemperature) createMCPServer() *server.MCPServer {
	// Create MCP server
	s := server.NewMCPServer(
		"apartment-temperature-server",
		"1.0.0",
		server.WithLogging(),
	)

	// Register tool: get_room_temperature
	getRoomTempTool := mcp.NewTool("get_room_temperature",
		mcp.WithDescription("Get the current temperature in a specific room"),
		mcp.WithString("room",
			mcp.Required(),
			mcp.Description("The room name (kitchen, bathroom, living room, bedroom, kids room)"),
			mcp.Enum(rooms...),
		),
	)

	s.AddTool(getRoomTempTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Type assert Arguments to map[string]interface{}
		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		roomVal, exists := args["room"]
		if !exists {
			return mcp.NewToolResultError("room parameter is required"), nil
		}

		room, ok := roomVal.(string)
		if !ok {
			return mcp.NewToolResultError("room parameter must be a string"), nil
		}

		if !isValidRoom(room) {
			return mcp.NewToolResultError(fmt.Sprintf("Unknown room '%s'. Available rooms: %v", room, rooms)), nil
		}

		temp := getRoomTemperature(room)
		message := fmt.Sprintf("The temperature in the %s is %.1f°C", room, temp)

		return mcp.NewToolResultText(message), nil
	})

	// Register tool: get_all_temperatures
	getAllTempTool := mcp.NewTool("get_all_temperatures",
		mcp.WithDescription("Get the current temperature in all rooms"),
	)

	s.AddTool(getAllTempTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result := "Current temperatures in all rooms:\n"

		for _, room := range rooms {
			temp := getRoomTemperature(room)
			result += fmt.Sprintf("%s: %.1f°C\n", room, temp)
		}

		return mcp.NewToolResultText(result), nil
	})

	return s
}

func (m *MCPTemperature) GetHTTPHandler() (http.Handler, error) {
	s := m.createMCPServer()
	httpServer := server.NewStreamableHTTPServer(s)
	return httpServer, nil
}

func (m *MCPTemperature) RunServer() error {
	s := m.createMCPServer()
	httpServer := server.NewStreamableHTTPServer(s)

	fmt.Printf("Starting temperature mcp server as StreamableHTTPServer on port 7752\n")
	if err := httpServer.Start("localhost:7752"); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPTemperature) RunClient(url string) error {
	if url == "" {
		url = "http://localhost:7752/mcp"
	}
	// Create a new StreamableHttpClient for the StreamableHTTPServer
	c, err := mgclient.NewStreamableHttpClient(url)
	if err != nil {
		log.Fatal(err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the transport first
	if err := c.Start(ctx); err != nil {
		log.Fatalf("Failed to start transport: %v", err)
	}

	// Initialize the connection
	initResult, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "temperature-client",
				Version: "1.0.0",
			},
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

	// Test 1: Get all temperatures
	fmt.Println("=== Getting all room temperatures ===")
	result, err := c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_all_temperatures",
			Arguments: map[string]interface{}{},
		},
	})
	if err != nil {
		log.Printf("Error calling tool: %v\n", err)
	} else if result.IsError {
		fmt.Printf("Error: %s\n", result.Content[0])
	} else {
		fmt.Println(result.Content[0])
	}

	// Test 2: Get individual room temperatures
	rooms := []string{"kitchen", "bathroom", "living room", "bedroom", "kids room"}

	fmt.Println("=== Getting individual room temperatures ===")
	for _, room := range rooms {
		result, err := c.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name: "get_room_temperature",
				Arguments: map[string]interface{}{
					"room": room,
				},
			},
		})

		if err != nil {
			log.Printf("Error calling tool for %s: %v\n", room, err)
			continue
		}

		if result.IsError {
			fmt.Printf("Error for %s: %s\n", room, result.Content[0])
		} else {
			fmt.Println(result.Content[0])
		}
	}
	fmt.Println()

	// Test 3: Try an invalid room (should get error)
	fmt.Println("=== Testing invalid room ===")
	result, err = c.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": "garage",
			},
		},
	})
	if err != nil {
		log.Printf("Error calling tool: %v\n", err)
	} else if result.IsError {
		fmt.Printf("Expected error received: %s\n", result.Content[0])
	} else {
		fmt.Println(result.Content[0])
	}
	fmt.Println()

	// Close the connection
	if err := c.Close(); err != nil {
		log.Printf("Error closing client: %v", err)
		return err
	}

	fmt.Println("Client finished successfully")
	return nil
}
