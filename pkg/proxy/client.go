package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

var ErrDisconnected = errors.New("client is disconnected")

// bearerAuthTransport wraps an http.RoundTripper to add Bearer token authorization
type bearerAuthTransport struct {
	wrapped http.RoundTripper
	bearer  string
}

func (t *bearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.bearer)
	return t.wrapped.RoundTrip(req)
}

type Client struct {
	cfg        MCPConfig
	client     *mcp.Client
	session    *mcp.ClientSession
	initResult *mcp.InitializeResult

	mutex       sync.Mutex
	isConnected bool
	isClosed    bool
	lastError   error
	errorCh     chan struct{}
	cancel      context.CancelFunc
	connectedCh chan struct{}
	log         *logrus.Entry
}

func NewClient(cfg MCPConfig) *Client {
	if cfg.PingInterval == 0 {
		cfg.PingInterval = 30 * time.Second
	}
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 5 * time.Second
	}

	log := logrus.WithFields(logrus.Fields{
		"client":    cfg.Name,
		"transport": cfg.Transport,
	})

	s := &Client{
		cfg:         cfg,
		errorCh:     make(chan struct{}),
		connectedCh: make(chan struct{}),
		log:         log,
	}

	return s
}

func (cs *Client) GetInitResult() *mcp.InitializeResult {
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
	cs.log.Infof("creating new client")

	// Create client if not exists
	if cs.client == nil {
		cs.client = mcp.NewClient(&mcp.Implementation{
			Name:    "mcp-auth-proxy-upstream-client",
			Version: "1.0.0",
		}, nil)
	}

	// Create transport based on config
	var transport mcp.Transport
	var err error

	switch cs.cfg.Transport {
	case "streamablehttp":
		streamableTransport := &mcp.StreamableClientTransport{
			Endpoint: cs.cfg.URL,
		}
		if cs.cfg.Bearer != "" {
			streamableTransport.HTTPClient = &http.Client{
				Transport: &bearerAuthTransport{
					wrapped: http.DefaultTransport,
					bearer:  cs.cfg.Bearer,
				},
			}
		}
		transport = streamableTransport
	case "stdio":
		transport = &mcp.CommandTransport{
			Command: exec.Command(cs.cfg.Cmd, cs.cfg.CmdArgs...),
		}
	default:
		return fmt.Errorf("unsupported transport: %s (supported: stdio, streamablehttp)", cs.cfg.Transport)
	}

	cs.log.Infof("connecting to server")
	session, err := cs.client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	// Get initialization result
	result := session.InitializeResult()

	cs.log.Infof("Connected to server: %s v%s",
		result.ServerInfo.Name,
		result.ServerInfo.Version)

	cs.log.Infof("Client capabilities: %+v", result.Capabilities.Tools)

	cs.mutex.Lock()
	cs.initResult = result
	cs.session = session
	cs.isConnected = true
	cs.isClosed = false

	// Close the connected channel to wake up all waiting goroutines
	close(cs.connectedCh)
	cs.mutex.Unlock()

	return nil
}

func (cs *Client) CallTool(
	ctx context.Context,
	params *mcp.CallToolParams,
) (*mcp.CallToolResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.CallToolResult, error) {
		return session.CallTool(ctx, params)
	})
}

func (cs *Client) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.ListToolsResult, error) {
		return session.ListTools(ctx, params)
	})
}

func (cs *Client) ListResources(ctx context.Context, params *mcp.ListResourcesParams) (*mcp.ListResourcesResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.ListResourcesResult, error) {
		return session.ListResources(ctx, params)
	})
}

func (cs *Client) ReadResource(ctx context.Context, params *mcp.ReadResourceParams) (*mcp.ReadResourceResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.ReadResourceResult, error) {
		return session.ReadResource(ctx, params)
	})
}

func (cs *Client) ListResourceTemplates(ctx context.Context, params *mcp.ListResourceTemplatesParams) (*mcp.ListResourceTemplatesResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.ListResourceTemplatesResult, error) {
		return session.ListResourceTemplates(ctx, params)
	})
}

func (cs *Client) GetPrompt(ctx context.Context, params *mcp.GetPromptParams) (*mcp.GetPromptResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.GetPromptResult, error) {
		return session.GetPrompt(ctx, params)
	})
}

func (cs *Client) ListPrompts(ctx context.Context, params *mcp.ListPromptsParams) (*mcp.ListPromptsResult, error) {
	return execute(cs, func(session *mcp.ClientSession) (*mcp.ListPromptsResult, error) {
		return session.ListPrompts(ctx, params)
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

	if cs.session != nil {
		return cs.session.Close()
	}

	return nil
}

func (cs *Client) WaitForConnection(ctx context.Context) error {
	cs.mutex.Lock()
	if cs.isConnected {
		cs.mutex.Unlock()
		return nil
	}
	connectedCh := cs.connectedCh
	cs.mutex.Unlock()

	select {
	case <-ctx.Done():
		return fmt.Errorf("context exceeded")
	case <-connectedCh:
		return nil
	}
}

func (cs *Client) getSession() *mcp.ClientSession {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	if !cs.isConnected {
		return nil
	}
	session := cs.session
	return session
}

func (cs *Client) ping(ctx context.Context) {
	timer := time.NewTimer(cs.cfg.PingInterval)
	for {
		timer.Reset(cs.cfg.PingInterval)

		select {
		case <-timer.C:
			session := cs.getSession()
			if session == nil {
				continue
			}
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := session.Ping(pingCtx, &mcp.PingParams{})
			cancel()
			if err != nil {
				logrus.Infof("Ping failed: %v", err)
				cs.setDisconnected(session, err)
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
			logrus.Infof("Reconnected successfully")
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
		session := cs.getSession()

		if session == nil {
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

func (cs *Client) setDisconnected(session *mcp.ClientSession, err error) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	if !cs.isConnected {
		return
	}
	if cs.session != session {
		return
	}

	// Close the old session to clean up resources
	if cs.session != nil {
		if closeErr := cs.session.Close(); closeErr != nil {
			logrus.WithError(closeErr).Error("Failed to close disconnected session")
		}
	}

	cs.session = nil
	cs.isConnected = false
	cs.lastError = err

	// Create a new channel for the next connection
	cs.connectedCh = make(chan struct{})

	select {
	case cs.errorCh <- struct{}{}:
	default:
		logrus.Warnf("errorCh not notified")
	}
}

func execute[T any](cs *Client, fn func(*mcp.ClientSession) (T, error)) (T, error) {
	session := cs.getSession()

	if session == nil {
		var zero T
		return zero, ErrDisconnected
	}

	var result T
	var err error

	for i := 0; i < 3; i++ {
		result, err = fn(session)
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

	cs.setDisconnected(session, err)
	var zero T
	return zero, fmt.Errorf("request failed after multiple retries: %w", err)
}
