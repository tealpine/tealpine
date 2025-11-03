package test

import (
	"github.com/google/gops/agent"
	"github.com/sirupsen/logrus"
	"log"
	"os"
	"testing"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

var (
	testConfig *mcp_proxy.Config
)

func getConfig(t *testing.T) *mcp_proxy.Config {
	if testConfig == nil {
		t.Fatalf("testConfig is nil")
	}
	return testConfig
}

func TestMain(m *testing.M) {
	if err := agent.Listen(agent.Options{}); err != nil {
		log.Fatal(err)
	}
	logrus.SetReportCaller(true)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
	})

	logrus.Infof("Setting up test environment...")

	var err error
	testConfig, err = mcp_proxy.ReadConfig("config.json")
	if err != nil {
		logrus.Fatal(err)
	}

	exitCode := m.Run()

	os.Exit(exitCode)
}
