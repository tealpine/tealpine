package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTempTokenStore(t *testing.T) (*TokenStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	return NewTokenStore(path), path
}

// --- Load ---

func TestTokenStore_Load_FileNotExist(t *testing.T) {
	ts, _ := newTempTokenStore(t)
	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestTokenStore_Load_ValidFile(t *testing.T) {
	ts, path := newTempTokenStore(t)

	stored := map[string]StoredToken{
		"myserver": {
			AccessToken:  "abc",
			RefreshToken: "xyz",
			TokenType:    "Bearer",
		},
	}
	data, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, "abc", tokens["myserver"].AccessToken)
	require.Equal(t, "xyz", tokens["myserver"].RefreshToken)
}

func TestTokenStore_Load_MalformedJSON(t *testing.T) {
	ts, path := newTempTokenStore(t)
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	_, err := ts.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse token file")
}

// --- Save ---

func TestTokenStore_Save_CreatesFile(t *testing.T) {
	ts, path := newTempTokenStore(t)

	err := ts.Save("server1", StoredToken{AccessToken: "tok1", TokenType: "Bearer"})
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err, "token file should exist after Save")
}

func TestTokenStore_Save_RoundTrip(t *testing.T) {
	ts, _ := newTempTokenStore(t)
	expiry := time.Now().Add(time.Hour).Truncate(time.Second)

	original := StoredToken{
		AccessToken:  "access",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		Expiry:       expiry,
		ClientID:     "client-id",
	}
	require.NoError(t, ts.Save("srv", original))

	tokens, err := ts.Load()
	require.NoError(t, err)
	got := tokens["srv"]
	require.Equal(t, original.AccessToken, got.AccessToken)
	require.Equal(t, original.RefreshToken, got.RefreshToken)
	require.Equal(t, original.TokenType, got.TokenType)
	require.Equal(t, original.ClientID, got.ClientID)
	require.True(t, original.Expiry.Equal(got.Expiry))
}

func TestTokenStore_Save_MergesWithExisting(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	require.NoError(t, ts.Save("server1", StoredToken{AccessToken: "tok1"}))
	require.NoError(t, ts.Save("server2", StoredToken{AccessToken: "tok2"}))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Len(t, tokens, 2)
	require.Equal(t, "tok1", tokens["server1"].AccessToken)
	require.Equal(t, "tok2", tokens["server2"].AccessToken)
}

func TestTokenStore_Save_OverwritesExistingEntry(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	require.NoError(t, ts.Save("srv", StoredToken{AccessToken: "old"}))
	require.NoError(t, ts.Save("srv", StoredToken{AccessToken: "new"}))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, "new", tokens["srv"].AccessToken)
}

func TestTokenStore_Save_NoTempFilesLeft(t *testing.T) {
	ts, path := newTempTokenStore(t)

	require.NoError(t, ts.Save("srv", StoredToken{AccessToken: "tok"}))

	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp", "no temp files should remain after Save")
	}
}

// --- Delete ---

func TestTokenStore_Delete_RemovesToken(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	require.NoError(t, ts.Save("srv", StoredToken{AccessToken: "tok"}))
	require.NoError(t, ts.Delete("srv"))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Empty(t, tokens)
}

func TestTokenStore_Delete_LeavesOthersIntact(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	require.NoError(t, ts.Save("keep", StoredToken{AccessToken: "keep-tok"}))
	require.NoError(t, ts.Save("remove", StoredToken{AccessToken: "remove-tok"}))
	require.NoError(t, ts.Delete("remove"))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Equal(t, "keep-tok", tokens["keep"].AccessToken)
}

func TestTokenStore_Delete_NonExistentKey(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	// File doesn't exist yet — should not error
	require.NoError(t, ts.Delete("ghost"))

	// File exists but key is absent — should not error
	require.NoError(t, ts.Save("other", StoredToken{AccessToken: "tok"}))
	require.NoError(t, ts.Delete("ghost"))

	tokens, err := ts.Load()
	require.NoError(t, err)
	require.Len(t, tokens, 1)
}

// --- Concurrency ---

func TestTokenStore_ConcurrentSaves(t *testing.T) {
	ts, _ := newTempTokenStore(t)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			name := "server"
			if i%2 == 0 {
				name = "other"
			}
			_ = ts.Save(name, StoredToken{AccessToken: "tok"})
		}(i)
	}
	wg.Wait()

	tokens, err := ts.Load()
	require.NoError(t, err)
	// Both keys must exist and the file must be valid JSON
	require.Contains(t, tokens, "server")
	require.Contains(t, tokens, "other")
}
