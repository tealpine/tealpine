package proxy

import (
	"github.com/google/gops/agent"
	"github.com/sirupsen/logrus"
	"log"
	"os"
	"testing"
)

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

	exitCode := m.Run()

	os.Exit(exitCode)
}
