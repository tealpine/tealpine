package main

import (
	"context"
	"github.com/sirupsen/logrus"
	"mcp-auth-proxy/pkg/proxy"
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

	cfg, err := proxy.ReadConfig("test/config.json")
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
