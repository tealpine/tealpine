package proxy

import (
	"context"
	"errors"
	"fmt"
	mgclient "github.com/mark3labs/mcp-go/client"
	mgmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/sirupsen/logrus"
	"log"
	"sync"
	"time"
)

var ErrDisconnected = errors.New("client is disconnected")

// ConnectionListener is notified when the client connects or reconnects
type ConnectionListener interface {
	OnConnected(initResult *mgmcp.InitializeResult) error
}

type Client struct {
	cfg        MCPConfig
	client     mgclient.MCPClient
	initResult *mgmcp.InitializeResult

	mutex       sync.Mutex
	isConnected bool
	isClosed    bool
	lastError   error
	errorCh     chan struct{}
	cancel      context.CancelFunc
	listeners   []ConnectionListener
}

func NewClient(cfg MCPConfig) *Client {
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 30 * time.Second
	}
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}

	s := &Client{
		cfg:     cfg,
		errorCh: make(chan struct{}),
	}

	return s
}

func (cs *Client) RegisterListener(listener ConnectionListener) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.listeners = append(cs.listeners, listener)
}

func (cs *Client) GetInitResult() *mgmcp.InitializeResult {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.initResult
}

func (cs *Client) IsConnected() bool {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.isConnected
}

func (cs *Client) Start(ctx context.Context) {
	clientCtx, cancel := context.WithCancel(ctx)
	cs.cancel = cancel
	go cs.keepConnect(clientCtx)
	go cs.ping(clientCtx)
}

func (cs *Client) init(ctx context.Context) error {

	logrus.Infof("creating new client")
	var err error
	var client mgclient.MCPClient
	switch cs.cfg.Transport {
	case "sse":
		client, err = mgclient.NewSSEMCPClient(cs.cfg.URL)
	case "streamablehttp":
		client, err = mgclient.NewStreamableHttpClient(cs.cfg.URL)
	case "stdio":
		client, err = mgclient.NewStdioMCPClient(cs.cfg.Cmd, []string{}, cs.cfg.CmdArgs...)
	}

	if err != nil {
		return err
	}

	if cs.cfg.Transport != "stdio" {
		// we need to do type assertion to call Start method
		type starter interface {
			Start(context.Context) error
		}
		if s, ok := client.(starter); ok {
			err := s.Start(ctx)
			if err != nil {
				if closeErr := client.Close(); closeErr != nil {
					logrus.WithError(closeErr).Error("Failed to close client after start failure")
				}
				return err
			}
		}
	}

	logrus.Infof("initializing client")
	result, err := client.Initialize(ctx, mgmcp.InitializeRequest{
		Params: mgmcp.InitializeParams{
			ProtocolVersion: mgmcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mgmcp.Implementation{
				Name:    "mcp-auth-proxy-upstream-client",
				Version: "1.0.0",
			},
		},
	})

	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("Failed to close client after initialization failure")
		}
		return fmt.Errorf("initialization failed: %w", err)
	}

	logrus.Infof("Connected to server: %s v%s",
		result.ServerInfo.Name,
		result.ServerInfo.Version)

	logrus.Infof("Client capabilities: %+v", result.Capabilities.Tools)

	cs.mutex.Lock()
	cs.initResult = result
	cs.isConnected = true
	cs.isClosed = false
	cs.client = client
	listeners := make([]ConnectionListener, len(cs.listeners))
	copy(listeners, cs.listeners)
	cs.mutex.Unlock()

	// Notify listeners outside the lock to avoid deadlock
	for _, listener := range listeners {
		if err := listener.OnConnected(result); err != nil {
			logrus.WithError(err).Error("Failed to notify listener of connection")
		}
	}

	return nil

}

func (cs *Client) CallTool(
	ctx context.Context,
	request mgmcp.CallToolRequest,
) (*mgmcp.CallToolResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.CallToolResult, error) {
		return client.CallTool(ctx, request)
	})
}

func (cs *Client) ListTools(ctx context.Context, request mgmcp.ListToolsRequest) (*mgmcp.ListToolsResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.ListToolsResult, error) {
		return cs.client.ListTools(ctx, request)
	})
}

func (cs *Client) ListResources(ctx context.Context, request mgmcp.ListResourcesRequest) (*mgmcp.ListResourcesResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.ListResourcesResult, error) {
		return client.ListResources(ctx, request)
	})
}

func (cs *Client) ReadResource(ctx context.Context, request mgmcp.ReadResourceRequest) (*mgmcp.ReadResourceResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.ReadResourceResult, error) {
		return client.ReadResource(ctx, request)
	})
}

func (cs *Client) ListResourceTemplates(ctx context.Context, request mgmcp.ListResourceTemplatesRequest) (*mgmcp.ListResourceTemplatesResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.ListResourceTemplatesResult, error) {
		return client.ListResourceTemplates(ctx, request)
	})
}

func (cs *Client) GetPrompt(ctx context.Context, request mgmcp.GetPromptRequest) (*mgmcp.GetPromptResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.GetPromptResult, error) {
		return client.GetPrompt(ctx, request)
	})
}

func (cs *Client) ListPrompts(ctx context.Context, request mgmcp.ListPromptsRequest) (*mgmcp.ListPromptsResult, error) {
	return execute(cs, func(client mgclient.MCPClient) (*mgmcp.ListPromptsResult, error) {
		return client.ListPrompts(ctx, request)
	})
}

func (cs *Client) Close() error {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if cs.isClosed {
		return nil
	}

	cs.isClosed = true
	cs.cancel()

	if cs.client != nil {
		return cs.client.Close()
	}

	return nil
}

//func (cs *Client) startHealthCheck() {
//	ticker := time.NewTicker(cs.cfg.PingInterval)
//	defer ticker.Stop()
//	logrus.Info("AAAAAAAAAAAAA", cs.cfg.PingInterval)
//
//	for {
//		logrus.Info("FFFFFFFFFFFFFFFFFFFF")
//		select {
//		case <-cs.doneCh:
//			return
//		case <-ticker.C:
//			cs.mutex.Lock()
//			if !cs.isConnected {
//				cs.mutex.Unlock()
//				cs.connectLoop()
//				logrus.Info("After reconnect")
//				continue
//			}
//
//			logrus.Info("1111111111")
//			client := cs.client
//			cs.mutex.Unlock()
//
//			logrus.Info("22222222")
//			if client == nil {
//				continue
//			}
//
//			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//			logrus.Info("listening tools in health check 1")
//			_, err := client.ListTools(ctx, mgmcp.ListToolsRequest{})
//			logrus.WithError(err).Info("listening tools in health check 2")
//			cancel()
//
//			if err != nil {
//				log.Printf("Ping failed: %v", err)
//				cs.setDisconnected(err)
//			}
//		}
//	}
//}

func (cs *Client) getClient() mgclient.MCPClient {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	if !cs.isConnected {
		return nil
	}
	client := cs.client
	return client
}

func (cs *Client) ping(ctx context.Context) {
	timer := time.NewTimer(cs.cfg.PingInterval)
	for {
		timer.Reset(cs.cfg.PingInterval)

		select {
		case <-timer.C:
			client := cs.getClient()
			if client == nil {
				continue
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := client.Ping(pingCtx)
			cancel()
			if err != nil {
				logrus.Infof("Ping failed: %v", err)
				cs.setDisconnected(client, err)
			}
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (cs *Client) connectLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	for {
		logrus.Infof("Attempting to reconnect...")
		err := cs.init(ctx)
		if err == nil {
			log.Println("Reconnected successfully")
			return
		}
		logrus.Infof("Reconnect failed: %v", err)
		timer := time.NewTimer(cs.cfg.ReconnectDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			timer.Stop()
		}
	}
}

func (cs *Client) keepConnect(ctx context.Context) {
	for {
		client := cs.getClient()

		if client == nil {
			cs.connectLoop(ctx)
		}

		select {
		case <-cs.errorCh:
			continue
		case <-ctx.Done():
			return
		}
	}
}

//func (cs *Client) setConnected(res *mgmcp.InitializeResult) {
//	cs.mutex.Lock()
//	defer cs.mutex.Unlock()
//
//	cs.isConnected = true
//	cs.initResult = res
//}

func (cs *Client) setDisconnected(client mgclient.MCPClient, err error) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if !cs.isConnected {
		return
	}
	if cs.client != client {
		return
	}

	// Close the old client to clean up resources
	if cs.client != nil {
		if closeErr := cs.client.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("Failed to close disconnected client")
		}
	}

	cs.client = nil
	cs.isConnected = false
	cs.lastError = err
	select {
	case cs.errorCh <- struct{}{}:
	default:
		logrus.Warnf("errorCh not notified")
	}
}

func execute[T any](cs *Client, fn func(mgclient.MCPClient) (T, error)) (T, error) {
	client := cs.getClient()

	if client == nil {
		var zero T
		return zero, ErrDisconnected
	}

	var result T
	var err error

	for i := 0; i < 3; i++ {
		result, err = fn(client)
		if err == nil {
			return result, nil
		}

		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			var zero T
			return zero, err
		}

		logrus.Infof("Request failed: %v. Retrying...", err)
		time.Sleep(1 * time.Second)
	}

	cs.setDisconnected(client, err)
	var zero T
	return zero, fmt.Errorf("request failed after multiple retries: %w", err)
}
