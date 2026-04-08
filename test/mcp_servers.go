package test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPTestServer interface {
	RunServer() error
	RunClient(string) error
	GetHTTPHandler() (http.Handler, error)
}

// MCPCalculator implements a simple calculator MCP server
type MCPCalculator struct {
	server *mcp.Server
}

var _ MCPTestServer = (*MCPCalculator)(nil)

func (m *MCPCalculator) GetHTTPHandler() (http.Handler, error) {
	m.server = m.createMCPServer()
	httpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return m.server
	}, nil)
	return httpHandler, nil
}

// CalculatorInput defines the input schema for the calculator tool
type CalculatorInput struct {
	Operation string  `json:"operation" jsonschema:"The operation to perform (add, subtract, multiply, divide)"`
	X         float64 `json:"x" jsonschema:"First number"`
	Y         float64 `json:"y" jsonschema:"Second number"`
}

// CalculatorOutput defines the output schema for the calculator tool
type CalculatorOutput struct {
	Result float64 `json:"result" jsonschema:"The calculation result"`
}

// calculatorHandler returns the calculator tool handler function
func (m *MCPCalculator) calculatorHandler() func(ctx context.Context, request *mcp.CallToolRequest, input CalculatorInput) (*mcp.CallToolResult, CalculatorOutput, error) {
	return func(ctx context.Context, request *mcp.CallToolRequest, input CalculatorInput) (*mcp.CallToolResult, CalculatorOutput, error) {
		var result float64
		switch input.Operation {
		case "add":
			result = input.X + input.Y
		case "subtract":
			result = input.X - input.Y
		case "multiply":
			result = input.X * input.Y
		case "divide":
			if input.Y == 0 {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{
						&mcp.TextContent{
							Text: "cannot divide by zero",
						},
					},
				}, CalculatorOutput{}, nil
			}
			result = input.X / input.Y
		default:
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("unknown operation: %s", input.Operation),
					},
				},
			}, CalculatorOutput{}, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("%.2f", result),
				},
			},
		}, CalculatorOutput{Result: result}, nil
	}
}

func (m *MCPCalculator) createMCPServer() *mcp.Server {
	// Create a new MCP server
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "Calculator Demo",
		Version: "1.0.0",
	}, nil)

	// Add the calculator tool using the typed handler
	calculatorTool := &mcp.Tool{
		Name:        "calculate",
		Description: "Perform basic arithmetic operations",
	}

	mcp.AddTool(s, calculatorTool, m.calculatorHandler())

	return s
}

// RenameCalculateToCount removes the "calculate" tool and adds a "count" tool with the same functionality
// This triggers a tools/list_changed notification
func (m *MCPCalculator) RenameCalculateToCount(ctx context.Context) error {
	if m.server == nil {
		return fmt.Errorf("server not initialized")
	}

	// Remove the old "calculate" tool (automatically emits notification)
	m.server.RemoveTools("calculate")

	// Add the new "count" tool with the same handler (automatically emits notification)
	countTool := &mcp.Tool{
		Name:        "count",
		Description: "Perform basic arithmetic operations",
	}

	mcp.AddTool(m.server, countTool, m.calculatorHandler())

	// Note: AddTool and RemoveTools automatically emit notifications
	return nil
}

func (m *MCPCalculator) RunServer() error {
	s := m.createMCPServer()
	httpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s
	}, nil)

	// Start the HTTP server
	log.Println("Starting calculator MCP StreamableHTTP server on http://localhost:7751/mcp")
	server := &http.Server{
		Addr:    "localhost:7751",
		Handler: httpHandler,
	}
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPCalculator) RunClient(url string) error {
	// Create a new StreamableHTTP client
	if url == "" {
		url = "http://localhost:7751/mcp"
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "calculator-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: url,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to the server
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("Connected to: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Println()

	// List available tools
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
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

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "calculate",
			Arguments: map[string]interface{}{
				"operation": calc.operation,
				"x":         calc.x,
				"y":         calc.y,
			},
		})

		if err != nil {
			log.Printf("Error calling tool: %v\n", err)
			continue
		}

		// Check if the result is an error
		if result.IsError {
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					fmt.Printf("  Error: %s\n", textContent.Text)
				}
			}
		} else {
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					fmt.Printf("  Result: %s\n", textContent.Text)
				}
			}
		}
		fmt.Println()
	}

	fmt.Println("Client finished successfully")
	return nil
}

// MCPHello implements a simple hello world MCP server for stdio
type MCPHello struct{}

var _ MCPTestServer = (*MCPHello)(nil)

func (m *MCPHello) GetHTTPHandler() (http.Handler, error) {
	return nil, fmt.Errorf("GetHTTPHandler not implemented for MCPHello (stdio only)")
}

// HelloInput defines the input schema
type HelloInput struct {
	Name string `json:"name" jsonschema:"Name of the person to greet"`
}

// HelloOutput defines the output schema
type HelloOutput struct {
	Greeting string `json:"greeting" jsonschema:"The greeting message"`
}

func (m *MCPHello) RunServer() error {
	// Create a new MCP server
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "Demo 🚀",
		Version: "1.0.0",
	}, nil)

	// Add tool
	tool := &mcp.Tool{
		Name:        "hello_world",
		Description: "Say hello to someone",
	}

	// Add tool handler using typed handler
	mcp.AddTool(s, tool, func(ctx context.Context, request *mcp.CallToolRequest, input HelloInput) (*mcp.CallToolResult, HelloOutput, error) {
		greeting := fmt.Sprintf("Hello, %s!", input.Name)
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: greeting,
				},
			},
		}, HelloOutput{Greeting: greeting}, nil
	})

	// Start the stdio server
	if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPHello) RunClient(string) error {
	fmt.Printf("%v\n", os.Args)
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "tealpine-upstream-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.CommandTransport{
		Command: exec.Command(os.Args[0], "server", "hello"),
	}

	ctx := context.Background()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer session.Close()

	result := session.InitializeResult()
	fmt.Printf("RESULT: %v\n", result)
	return nil
}

// MCPFullServer implements an MCP server that exposes tools, resources,
// resource templates, and prompts — used to test proxy coverage of all
// capability types.
type MCPFullServer struct{}

var _ MCPTestServer = (*MCPFullServer)(nil)

func (m *MCPFullServer) GetHTTPHandler() (http.Handler, error) {
	s := m.createMCPServer()
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s
	}, nil)
	return h, nil
}

func (m *MCPFullServer) createMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "full-server", Version: "1.0.0"}, nil)

	// Tool: echo
	echoTool := &mcp.Tool{
		Name:        "echo",
		Description: "Echoes the input text",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
	}
	s.AddTool(echoTool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if len(req.Params.Arguments) > 0 {
			_ = json.Unmarshal(req.Params.Arguments, &args)
		}
		text, _ := args["text"].(string)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: text}},
		}, nil
	})

	// Resource: res://full/note
	s.AddResource(&mcp.Resource{
		URI:         "res://full/note",
		Name:        "note",
		Description: "A test note resource",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: "note content"}},
		}, nil
	})

	// Resource template: res://full/{id}
	s.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "res://full/{id}",
		Name:        "item",
		Description: "A test resource template",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: "item: " + req.Params.URI}},
		}, nil
	})

	// Prompt: greet
	s.AddPrompt(&mcp.Prompt{
		Name:        "greet",
		Description: "A greeting prompt",
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "Greeting",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: "Hello!"}},
			},
		}, nil
	})

	return s
}

func (m *MCPFullServer) RunServer() error {
	s := m.createMCPServer()
	h := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return s }, nil)
	srv := &http.Server{Addr: "localhost:7753", Handler: h}
	return srv.ListenAndServe()
}

func (m *MCPFullServer) RunClient(string) error { return fmt.Errorf("not implemented") }

// MCPTemperature implements a temperature monitoring MCP server
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

// RoomTemperatureInput defines the input schema
type RoomTemperatureInput struct {
	Room string `json:"room" jsonschema:"The room name"`
}

// TemperatureOutput defines the output schema
type TemperatureOutput struct {
	Temperature float64 `json:"temperature" jsonschema:"The temperature in Celsius"`
	Room        string  `json:"room" jsonschema:"The room name"`
}

func (m *MCPTemperature) createMCPServer() *mcp.Server {
	// Create MCP server
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "apartment-temperature-server",
		Version: "1.0.0",
	}, nil)

	// Register tool: get_room_temperature
	getRoomTempTool := &mcp.Tool{
		Name:        "get_room_temperature",
		Description: "Get the current temperature in a specific room",
	}

	mcp.AddTool(s, getRoomTempTool, func(ctx context.Context, request *mcp.CallToolRequest, input RoomTemperatureInput) (*mcp.CallToolResult, TemperatureOutput, error) {
		if !isValidRoom(input.Room) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("Unknown room '%s'. Available rooms: %v", input.Room, rooms),
					},
				},
			}, TemperatureOutput{}, nil
		}

		temp := getRoomTemperature(input.Room)
		message := fmt.Sprintf("The temperature in the %s is %.1f°C", input.Room, temp)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: message,
				},
			},
		}, TemperatureOutput{Temperature: temp, Room: input.Room}, nil
	})

	// Register tool: get_all_temperatures
	getAllTempTool := &mcp.Tool{
		Name:        "get_all_temperatures",
		Description: "Get the current temperature in all rooms",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}

	// For tools with no input, use map[string]any
	s.AddTool(getAllTempTool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result := "Current temperatures in all rooms:\n"

		for _, room := range rooms {
			temp := getRoomTemperature(room)
			result += fmt.Sprintf("%s: %.1f°C\n", room, temp)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: result,
				},
			},
		}, nil
	})

	return s
}

func (m *MCPTemperature) GetHTTPHandler() (http.Handler, error) {
	s := m.createMCPServer()
	httpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s
	}, nil)
	return httpHandler, nil
}

func (m *MCPTemperature) RunServer() error {
	s := m.createMCPServer()
	httpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return s
	}, nil)

	fmt.Printf("Starting temperature MCP server as StreamableHTTPServer on http://localhost:7752/mcp\n")
	server := &http.Server{
		Addr:    "localhost:7752",
		Handler: httpHandler,
	}
	if err := server.ListenAndServe(); err != nil {
		fmt.Printf("Server error: %v\n", err)
		return err
	}
	return nil
}

func (m *MCPTemperature) RunClient(url string) error {
	if url == "" {
		url = "http://localhost:7752/mcp"
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "temperature-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: url,
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to the server
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	initResult := session.InitializeResult()
	fmt.Printf("Connected to: %s v%s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Println()

	// List available tools
	toolsResult, err := session.ListTools(ctx, &mcp.ListToolsParams{})
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
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_all_temperatures",
		Arguments: map[string]interface{}{},
	})
	if err != nil {
		log.Printf("Error calling tool: %v\n", err)
	} else if result.IsError {
		for _, content := range result.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fmt.Printf("Error: %s\n", textContent.Text)
			}
		}
	} else {
		for _, content := range result.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fmt.Println(textContent.Text)
			}
		}
	}

	// Test 2: Get individual room temperatures
	testRooms := []string{"kitchen", "bathroom", "living room", "bedroom", "kids room"}

	fmt.Println("=== Getting individual room temperatures ===")
	for _, room := range testRooms {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_room_temperature",
			Arguments: map[string]interface{}{
				"room": room,
			},
		})

		if err != nil {
			log.Printf("Error calling tool for %s: %v\n", room, err)
			continue
		}

		if result.IsError {
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					fmt.Printf("Error for %s: %s\n", room, textContent.Text)
				}
			}
		} else {
			for _, content := range result.Content {
				if textContent, ok := content.(*mcp.TextContent); ok {
					fmt.Println(textContent.Text)
				}
			}
		}
	}
	fmt.Println()

	// Test 3: Try an invalid room (should get error)
	fmt.Println("=== Testing invalid room ===")
	result, err = session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_room_temperature",
		Arguments: map[string]interface{}{
			"room": "garage",
		},
	})
	if err != nil {
		log.Printf("Error calling tool: %v\n", err)
	} else if result.IsError {
		for _, content := range result.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fmt.Printf("Expected error received: %s\n", textContent.Text)
			}
		}
	} else {
		for _, content := range result.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				fmt.Println(textContent.Text)
			}
		}
	}
	fmt.Println()

	fmt.Println("Client finished successfully")
	return nil
}
