package main

import (
	"context"
	"fmt"
	"log"
	pkgclient "mcp-auth-proxy/pkg/client"
	"mcp-auth-proxy/test"
	"net/http"
	"os"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 3 {
		println("usage: " + os.Args[0] + " server|client calc|temp|hello|list [url]")
		os.Exit(1)
	}

	var mcp test.MCPTestServer
	switch os.Args[2] {
	case "calc":
		mcp = &test.MCPCalculator{}
	case "temp":
		mcp = &test.MCPTemperature{}
	case "hello":
		mcp = &test.MCPHello{}
	case "list":
		mcp = &MCPList{}
	default:
		println("wrong mcp name")
		return
	}

	var err error
	switch os.Args[1] {
	case "server":
		err = mcp.RunServer()
	case "client":
		url := ""
		if len(os.Args) == 4 {
			url = os.Args[3]
		}
		err = mcp.RunClient(url)
	default:
		println("wrong cmd name")
		return
	}
	if err != nil {
		println(err.Error())
	}
}

type MCPList struct{}

func (m *MCPList) RunServer() error {
	return fmt.Errorf("MCPList supports only client mode")
}

func (m *MCPList) RunClient(url string) error {
	// Create a new StreamableHTTP client
	if url == "" {
		url = "http://localhost:8088/multi/mcp"
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "generic-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint: url,
	}

	transport.HTTPClient = &http.Client{
		Transport: &pkgclient.BearerAuthTransport{
			Wrapped: http.DefaultTransport,
			Bearer:  "alicetoken",
		},
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
	return nil
}

func (m *MCPList) GetHTTPHandler() (http.Handler, error) {
	return nil, fmt.Errorf("MCPList supports only client mode")
}
