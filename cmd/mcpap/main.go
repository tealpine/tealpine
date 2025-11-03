package main

import (
	"context"
	"github.com/sirupsen/logrus"
	"mcp-auth-proxy/pkg/mcp_proxy"
)

func main() {
	println("MCP-AUTH-PROXY")

	logrus.SetReportCaller(true)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
	})

	config, err := mcp_proxy.ReadConfig("test/config.json")
	if err != nil {
		logrus.Fatal(err)
	}

	ctx := context.Background()

	_ = mcp_proxy.NewServer(ctx, config)
}
