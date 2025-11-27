package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"mcp-auth-proxy/pkg/proxy"
)

func main() {
	println("MCP-AUTH-PROXY")

	// Define command-line flags
	configFile := flag.String("config", "./config.json", "Path to configuration file")
	flag.Parse()

	logrus.SetReportCaller(true)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
	})

	// Check if config file exists
	if _, err := os.Stat(*configFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: Configuration file '%s' does not exist\n", *configFile)
		fmt.Fprintf(os.Stderr, "Usage: %s --config <path-to-config-file>\n", os.Args[0])
		os.Exit(1)
	}

	cfg, err := proxy.ReadConfig(*configFile)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.Debugf("%+v", cfg)

	ctx := context.Background()

	server := proxy.NewServer(cfg)

	if err = server.Init(ctx); err != nil {
		logrus.Fatal(err)
	}

	if err := server.Run(); err != nil {
		logrus.Fatal(err)
	}

}
