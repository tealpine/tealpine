package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// StoredToken represents a persisted OAuth token for an upstream MCP server
type StoredToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry,omitempty"`
	ClientID     string    `json:"client_id,omitempty"` // persisted from dynamic registration
}

// TokenStore manages reading/writing tokens.json
type TokenStore struct {
	path string
	mu   sync.Mutex
}

// NewTokenStore creates a new TokenStore for the given file path
func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

// Load reads the token file and returns the token map.
// Returns an empty map if the file does not exist.
func (ts *TokenStore) Load() (map[string]StoredToken, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return ts.loadLocked()
}

func (ts *TokenStore) loadLocked() (map[string]StoredToken, error) {
	data, err := os.ReadFile(ts.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]StoredToken), nil
		}
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	tokens := make(map[string]StoredToken)
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return tokens, nil
}

// Save persists a token for the given MCP name using read-merge-write with atomic rename.
func (ts *TokenStore) Save(name string, token StoredToken) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	tokens, err := ts.loadLocked()
	if err != nil {
		return err
	}

	tokens[name] = token
	return ts.writeLocked(tokens)
}

// Delete removes the token for the given MCP name.
func (ts *TokenStore) Delete(name string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	tokens, err := ts.loadLocked()
	if err != nil {
		return err
	}

	delete(tokens, name)
	return ts.writeLocked(tokens)
}

func (ts *TokenStore) writeLocked(tokens map[string]StoredToken) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal tokens: %w", err)
	}

	dir := filepath.Dir(ts.path)
	tmpFile, err := os.CreateTemp(dir, ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, ts.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}
