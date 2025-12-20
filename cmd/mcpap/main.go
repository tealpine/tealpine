package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tealpine/pkg/config"
	"tealpine/pkg/server"

	"github.com/sirupsen/logrus"
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

	cfg, err := config.ReadConfig(*configFile)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.Debugf("%+v", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	s := server.NewServer(cfg)

	if err = s.Init(ctx); err != nil {
		logrus.Fatal(err)
	}

	// Run server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Run()
	}()

	// Wait for either an error or a termination signal
	select {
	case err := <-errChan:
		if err != nil {
			logrus.Errorf("Server error: %v", err)
		}
	case sig := <-sigChan:
		logrus.Infof("Received signal: %v, shutting down gracefully...", sig)
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := s.Stop(stopCtx); err != nil {
			logrus.Errorf("Error stopping server: %v", err)
		}
		stopCancel()
	}

	logrus.Info("Server stopped")
}
