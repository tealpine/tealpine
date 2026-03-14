package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"tealpine/pkg/auth"
	"tealpine/pkg/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
)

var (
	ErrDisconnected  = errors.New("client is disconnected")
	ErrLoginRequired = errors.New("upstream login required")
)

// EventType represents the type of client event
type EventType int

const (
	EventConnected EventType = iota
	EventDisconnected
	EventToolsListChanged
	EventResourcesListChanged
	EventPromptsListChanged
	EventResourceTemplatesListChanged
	EventLoginRequired
)

// String returns the string representation of the EventType
func (et EventType) String() string {
	switch et {
	case EventConnected:
		return "Connected"
	case EventDisconnected:
		return "Disconnected"
	case EventToolsListChanged:
		return "ToolsListChanged"
	case EventResourcesListChanged:
		return "ResourcesListChanged"
	case EventPromptsListChanged:
		return "PromptsListChanged"
	case EventResourceTemplatesListChanged:
		return "ResourceTemplatesListChanged"
	case EventLoginRequired:
		return "LoginRequired"
	default:
		return "Unknown"
	}
}

// ClientEvent represents a client state change event
type ClientEvent struct {
	Type      EventType
	Timestamp time.Time
	Error     error // Only set for Disconnected events
}

// BearerAuthTransport wraps an http.RoundTripper to add Bearer token authorization
type BearerAuthTransport struct {
	Wrapped http.RoundTripper
	Bearer  string
}

func (t *BearerAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.Bearer)
	return t.Wrapped.RoundTrip(req)
}

type Client struct {
	cfg        config.UpstreamConfig
	client     *mcp.Client
	session    *mcp.ClientSession
	initResult *mcp.InitializeResult

	mutex         sync.Mutex
	isConnected   bool
	isClosed      bool
	loginRequired bool
	authInfo      *auth.UpstreamAuthInfo
	lastError     error
	errorCh       chan struct{}
	cancel        context.CancelFunc
	eventCh       chan ClientEvent
	lastEvent     ClientEvent
	log           *logrus.Entry
}

func NewClient(cfg config.UpstreamConfig) *Client {
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
		cfg:       cfg,
		errorCh:   make(chan struct{}),
		eventCh:   make(chan ClientEvent),
		lastEvent: ClientEvent{},
		log:       log,
	}

	return s
}

func (cs *Client) GetInitResult() *mcp.InitializeResult {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.initResult
}

// GetLastEvent returns the last event that occurred
// Returns nil if no events have occurred yet
func (cs *Client) GetLastEvent() *ClientEvent {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	if cs.lastEvent.Type == 0 && cs.lastEvent.Timestamp.IsZero() {
		return nil
	}
	// Return copy to prevent external mutation
	event := cs.lastEvent
	return &event
}

func (cs *Client) IsConnected() bool {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.isConnected
}

// IsLoginRequired returns true if the upstream MCP server requires OIDC login
func (cs *Client) IsLoginRequired() bool {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.loginRequired
}

// GetAuthInfo returns the discovered upstream auth info, or nil
func (cs *Client) GetAuthInfo() *auth.UpstreamAuthInfo {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.authInfo
}

// SetBearerToken updates the bearer token, clears loginRequired, and signals reconnect
func (cs *Client) SetBearerToken(token string) {
	cs.mutex.Lock()
	cs.cfg.Bearer = token
	cs.loginRequired = false
	cs.authInfo = nil
	cs.mutex.Unlock()

	// Signal the reconnection loop to try again
	select {
	case cs.errorCh <- struct{}{}:
	default:
	}
}

func (cs *Client) Start(ctx context.Context) {
	clientCtx, cancel := context.WithCancel(ctx)
	cs.cancel = cancel
	go cs.keepConnect(clientCtx)
	go cs.ping(clientCtx)
}

// GetConfig returns the client's upstream config (used by server for building redirect URLs)
func (cs *Client) GetConfig() config.UpstreamConfig {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	return cs.cfg
}

func (cs *Client) init(ctx context.Context) error {
	cs.log.Infof("creating new client")

	// For streamablehttp without a bearer token, probe for 401
	if cs.cfg.Transport == "streamablehttp" && cs.cfg.Bearer == "" {
		needs401, err := auth.ProbeUpstreamAuth(cs.cfg.URL)
		if err != nil {
			cs.log.Warnf("failed to probe upstream auth: %v", err)
			// Continue anyway — let the MCP connection attempt handle it
		} else if needs401 {
			return cs.handleUpstreamAuthRequired()
		}
	}

	// Create client if not exists
	if cs.client == nil {
		cs.client = mcp.NewClient(&mcp.Implementation{
			Name:    "tealpine-upstream-client",
			Version: "1.0.0",
		}, &mcp.ClientOptions{
			ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
				cs.log.Debugf("Received tools/list_changed notification")
				cs.emitEvent(ClientEvent{
					Type:      EventToolsListChanged,
					Timestamp: time.Now(),
					Error:     nil,
				})
			},
			ResourceListChangedHandler: func(ctx context.Context, req *mcp.ResourceListChangedRequest) {
				cs.log.Debugf("Received resources/list_changed notification")
				cs.emitEvent(ClientEvent{
					Type:      EventResourcesListChanged,
					Timestamp: time.Now(),
					Error:     nil,
				})
			},
			PromptListChangedHandler: func(ctx context.Context, req *mcp.PromptListChangedRequest) {
				cs.log.Debugf("Received prompts/list_changed notification")
				cs.emitEvent(ClientEvent{
					Type:      EventPromptsListChanged,
					Timestamp: time.Now(),
					Error:     nil,
				})
			},
		})
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
				Transport: &BearerAuthTransport{
					Wrapped: http.DefaultTransport,
					Bearer:  cs.cfg.Bearer,
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
		// If streamablehttp connection failed with 401, trigger upstream auth flow
		if cs.cfg.Transport == "streamablehttp" && strings.Contains(err.Error(), "401") {
			return cs.handleUpstreamAuthRequired()
		}
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
	cs.mutex.Unlock()

	// Broadcast Connected event
	cs.emitEvent(ClientEvent{
		Type:      EventConnected,
		Timestamp: time.Now(),
		Error:     nil,
	})

	return nil
}

// emitEvent broadcasts an event to all waiting goroutines
func (cs *Client) emitEvent(event ClientEvent) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.emitEventLocked(event)
}

// emitEventLocked broadcasts an event (assumes mutex is already locked)
func (cs *Client) emitEventLocked(event ClientEvent) {
	cs.lastEvent = event
	close(cs.eventCh)
	cs.eventCh = make(chan ClientEvent)
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

	// Close session before cancelling context so cleanup requests
	// (e.g. DELETE for streamablehttp) can complete
	var err error
	if cs.session != nil {
		err = cs.session.Close()
	}

	cs.cancel()
	return err
}

// WaitForEvent waits for a client event matching the specified types
// If no eventTypes are provided, waits for any event
// Returns the event that occurred or an error if context is canceled
func (cs *Client) WaitForEvent(ctx context.Context, eventTypes ...EventType) (*ClientEvent, error) {
	// If no types specified, accept any event
	if len(eventTypes) == 0 {
		eventTypes = []EventType{
			EventConnected,
			EventDisconnected,
			EventToolsListChanged,
			EventResourcesListChanged,
			EventPromptsListChanged,
			EventResourceTemplatesListChanged,
		}
	}

	// Helper function to check if event type matches filter
	matchesFilter := func(eventType EventType) bool {
		for _, t := range eventTypes {
			if t == eventType {
				return true
			}
		}
		return false
	}

	cs.mutex.Lock()
	// Get the current event channel to watch
	eventCh := cs.eventCh
	cs.mutex.Unlock()

	// Wait for either context cancellation or next event
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context exceeded")
	case <-eventCh:
		// Event channel was closed, meaning a new event occurred
		// Retrieve the new event with mutex protection
		cs.mutex.Lock()
		newEvent := cs.lastEvent
		cs.mutex.Unlock()

		// Check if the event matches our filter
		if matchesFilter(newEvent.Type) {
			return &newEvent, nil
		}

		// Event doesn't match filter, recursively wait for next event
		// This handles rapid connect/disconnect cycles
		return cs.WaitForEvent(ctx, eventTypes...)
	}
}

// WaitForConnection waits for the client to connect
// This is a convenience wrapper around WaitForEvent that only waits for Connected events
// Maintains backward compatibility with existing code
func (cs *Client) WaitForConnection(ctx context.Context) error {
	if cs.IsConnected() {
		return nil
	}
	_, err := cs.WaitForEvent(ctx, EventConnected)
	return err
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

// handleUpstreamAuthRequired discovers auth info and marks the client as needing login.
// Always returns ErrLoginRequired so connectLoop stops retrying.
// If discovery fails, login_required is still set — discovery can be retried from the login handler.
func (cs *Client) handleUpstreamAuthRequired() error {
	cs.log.Infof("upstream server requires authentication, discovering auth endpoints")

	// Determine client_id override from config
	var clientIDOverride string
	if cs.cfg.Auth != nil && cs.cfg.Auth.ClientID != "" {
		clientIDOverride = cs.cfg.Auth.ClientID
	}

	authInfo, err := auth.DiscoverAndRegister(cs.cfg.URL, "", clientIDOverride)
	if err != nil {
		cs.log.Warnf("upstream auth discovery failed: %v", err)
		// Still enter login_required — discovery can be retried from login handler
	}

	cs.mutex.Lock()
	cs.authInfo = authInfo // may be nil if discovery failed
	cs.loginRequired = true
	cs.mutex.Unlock()

	cs.emitEvent(ClientEvent{
		Type:      EventLoginRequired,
		Timestamp: time.Now(),
	})

	return ErrLoginRequired
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
		if errors.Is(err, ErrLoginRequired) {
			logrus.Infof("Upstream login required, stopping reconnect loop")
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

	// Broadcast Disconnected event
	cs.emitEventLocked(ClientEvent{
		Type:      EventDisconnected,
		Timestamp: time.Now(),
		Error:     err,
	})

	// Keep errorCh signal for reconnection loop
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
