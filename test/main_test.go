package test

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"mcp-auth-proxy/pkg/mcp_proxy"
)

var (
	testConfig *mcp_proxy.Config
	cmds       []*exec.Cmd
)

func getConfig(t *testing.T) *mcp_proxy.Config {
	if testConfig == nil {
		t.Fatalf("testConfig is nil")
	}
	return testConfig
}

func TestMain(m *testing.M) {
	fmt.Println("Setting up test environment...")
	logrus.SetReportCaller(true)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		FullTimestamp:   true,
	})

	err := setupTestEnvironment()
	if err != nil {
		fmt.Printf("Failed to setup test environment: %v\n", err)
		os.Exit(1)
	}

	exitCode := m.Run()

	fmt.Println("Tearing down test environment...")

	teardownTestEnvironment()

	os.Exit(exitCode)
}

func setupTestEnvironment() error {
	var err error
	testConfig, err = mcp_proxy.ReadConfig("config.json")
	if err != nil {
		return err
	}
	calc := runCmd("mcp_servers/calculator")
	temp := runCmd("mcp_servers/temperature")
	time.Sleep(time.Second)

	cmds = append(cmds, calc, temp)

	return nil
}

func teardownTestEnvironment() {
	for _, cmd := range cmds {
		if err := cmd.Process.Kill(); err != nil {
			fmt.Printf("Failed to kill test command: %v\n", err)
		}
	}
}

func runCmd(path string) *exec.Cmd {
	cmd := exec.Command("go", "run", "main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = path

	go func() {
		err := cmd.Start()
		if err != nil {
			log.Fatalf("Failed to run temperature command: %v\n", err)
		}
	}()

	return cmd
}
