package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "tealpine_session"
	sessionIDLength   = 32    // 32 bytes = 64 hex chars
	stateTokenLength  = 32    // 32 bytes for CSRF state
	verifierMinLength = 43    // Minimum PKCE verifier length
	verifierMaxLength = 128   // Maximum PKCE verifier length
	sessionMaxAge     = 86400 // 24 hours in seconds
)

// SessionStore manages user sessions in memory
type SessionStore struct {
	sessions map[string]*SessionData
	mu       sync.RWMutex
}

// SessionData holds session information
type SessionData struct {
	Username     string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	State        string // CSRF token for OAuth flow
	CodeVerifier string // PKCE verifier for OAuth flow
}

// NewSessionStore creates a new in-memory session store
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*SessionData),
	}

	// Start background cleanup goroutine
	go store.cleanupExpired()

	return store
}

// CreateSession creates a new session for the given username
func (s *SessionStore) CreateSession(username string) (string, error) {
	sessionID, err := generateSecureToken(sessionIDLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate session ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[sessionID] = &SessionData{
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	return sessionID, nil
}

// GetSession retrieves a session by ID
func (s *SessionStore) GetSession(sessionID string) (*SessionData, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		return nil, false
	}

	return session, true
}

// DeleteSession removes a session by ID
func (s *SessionStore) DeleteSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
}

// StoreOAuthState stores OAuth flow data in a session
func (s *SessionStore) StoreOAuthState(sessionID, state, codeVerifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found")
	}

	session.State = state
	session.CodeVerifier = codeVerifier

	return nil
}

// GetOAuthState retrieves OAuth flow data from a session
func (s *SessionStore) GetOAuthState(sessionID string) (state, codeVerifier string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return "", "", fmt.Errorf("session not found")
	}

	return session.State, session.CodeVerifier, nil
}

// cleanupExpired removes expired sessions periodically
func (s *SessionStore) cleanupExpired() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for id, session := range s.sessions {
			if now.After(session.ExpiresAt) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// SetSessionCookie sets a session cookie in the HTTP response
func SetSessionCookie(w http.ResponseWriter, sessionID string) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure should be true in production (HTTPS)
		// For development with HTTP, set to false
		Secure: false, // TODO: implement dev/prod mode
	}
	http.SetCookie(w, cookie)
}

// GetSessionCookie retrieves the session ID from the HTTP request cookie
func GetSessionCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", err
	}
	return cookie.Value, nil
}

// ClearSessionCookie clears the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
	}
	http.SetCookie(w, cookie)
}

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// GeneratePKCEVerifier generates a PKCE code verifier (43-128 characters, base64url encoded)
func GeneratePKCEVerifier() (string, error) {
	// Generate 64 random bytes (will be ~86 chars when base64url encoded)
	bytes := make([]byte, 64)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// GenerateStateToken generates a CSRF state token
func GenerateStateToken() (string, error) {
	return generateSecureToken(stateTokenLength)
}
