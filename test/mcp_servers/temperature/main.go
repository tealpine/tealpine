package main

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"log"
	"math/rand"
	"os"
	"time"
)

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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "client" {
		cli()
		return
	}
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

	httpServer := server.NewStreamableHTTPServer(s)

	fmt.Printf("Starting temperature mcp server as StreamableHTTPServer on port 7752\n")
	if err := httpServer.Start("localhost:7752"); err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
}

func cli() {
	// Create a new StreamableHttpClient for the StreamableHTTPServer
	c, err := client.NewStreamableHttpClient("http://localhost:7752/mcp")
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
	}

	fmt.Println("Client finished successfully")
}
