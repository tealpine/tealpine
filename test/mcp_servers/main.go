package main

import (
	"mcp-auth-proxy/test"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		println("usage: " + os.Args[0] + " calc|temp|hello server|client [url]")
		os.Exit(1)
	}

	var mcp test.MCPTestServer
	switch os.Args[1] {
	case "calc":
		mcp = &test.MCPCalculator{}
	case "temp":
		mcp = &test.MCPTemperature{}
	case "hello":
		mcp = &test.MCPHello{}
	default:
		println("wrong mcp name")
		return
	}

	var err error
	switch os.Args[2] {
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
