package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	OAuthCallbackPort = 19876
	OAuthCallbackPath = "/mcp/oauth/callback"
)

type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scope        string
	AuthURL      string
	TokenURL     string
}

type Tokens struct {
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken,omitempty"`
	ExpiresAt    float64 `json:"expiresAt,omitempty"`
	Scope        string  `json:"scope,omitempty"`
}

type ClientInfo struct {
	ClientID              string  `json:"clientId"`
	ClientSecret          string  `json:"clientSecret,omitempty"`
	ClientIDIssuedAt      float64 `json:"clientIdIssuedAt,omitempty"`
	ClientSecretExpiresAt float64 `json:"clientSecretExpiresAt,omitempty"`
}

type AuthEntry struct {
	Tokens       *Tokens     `json:"tokens,omitempty"`
	ClientInfo   *ClientInfo `json:"clientInfo,omitempty"`
	CodeVerifier string      `json:"codeVerifier,omitempty"`
	OAuthState   string      `json:"oauthState,omitempty"`
	ServerURL    string      `json:"serverUrl,omitempty"`
}

type AuthStore struct {
	mu       sync.RWMutex
	path     string
	entries  map[string]AuthEntry
	modified bool
}

func NewAuthStore(dataDir string) (*AuthStore, error) {
	path := filepath.Join(dataDir, "mcp-auth.json")
	store := &AuthStore{
		path:    path,
		entries: make(map[string]AuthEntry),
	}
	if err := store.load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return store, nil
}

func (s *AuthStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *AuthStore) save() error {
	if !s.modified {
		return nil
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func (s *AuthStore) Get(mcpName string) (AuthEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[mcpName]
	return entry, ok
}

func (s *AuthStore) GetForURL(mcpName, serverURL string) (AuthEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[mcpName]
	if !ok || entry.ServerURL == "" {
		return AuthEntry{}, false
	}
	if entry.ServerURL != serverURL {
		return AuthEntry{}, false
	}
	return entry, true
}

func (s *AuthStore) Set(mcpName string, entry AuthEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[mcpName] = entry
	s.modified = true
	return s.save()
}

func (s *AuthStore) UpdateTokens(mcpName string, tokens *Tokens, serverURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[mcpName]
	entry.Tokens = tokens
	if serverURL != "" {
		entry.ServerURL = serverURL
	}
	s.entries[mcpName] = entry
	s.modified = true
	return s.save()
}

func (s *AuthStore) UpdateClientInfo(mcpName string, clientInfo *ClientInfo, serverURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[mcpName]
	entry.ClientInfo = clientInfo
	if serverURL != "" {
		entry.ServerURL = serverURL
	}
	s.entries[mcpName] = entry
	s.modified = true
	return s.save()
}

func (s *AuthStore) UpdateCodeVerifier(mcpName, codeVerifier string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[mcpName]
	entry.CodeVerifier = codeVerifier
	s.entries[mcpName] = entry
	s.modified = true
	return s.save()
}

func (s *AuthStore) UpdateOAuthState(mcpName, oauthState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[mcpName]
	entry.OAuthState = oauthState
	s.entries[mcpName] = entry
	s.modified = true
	return s.save()
}

func (s *AuthStore) Remove(mcpName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, mcpName)
	s.modified = true
	return s.save()
}

func (s *AuthStore) IsTokenExpired(mcpName string) (bool, error) {
	s.mu.RLock()
	entry, ok := s.entries[mcpName]
	s.mu.RUnlock()
	if !ok || entry.Tokens == nil {
		return false, nil
	}
	if entry.Tokens.ExpiresAt == 0 {
		return false, nil
	}
	return entry.Tokens.ExpiresAt < float64(time.Now().Unix()), nil
}

type OAuthProvider interface {
	RedirectURL() string
	ClientMetadata() ClientMetadata
	ClientInformation(ctx context.Context) (*ClientInfo, error)
	SaveClientInformation(ctx context.Context, info *ClientInfo) error
	Tokens(ctx context.Context) (*Tokens, error)
	SaveTokens(ctx context.Context, tokens *Tokens) error
	RedirectToAuthorization(ctx context.Context, authURL *url.URL) error
	SaveCodeVerifier(ctx context.Context, verifier string) error
	CodeVerifier(ctx context.Context) (string, error)
	SaveState(ctx context.Context, state string) error
	State(ctx context.Context) (string, error)
	InvalidateCredentials(ctx context.Context, typ CredentialType) error
	RefreshAccessToken(ctx context.Context, refreshToken string) (*Tokens, error)
}

type CredentialType string

const (
	CredentialTypeAll    CredentialType = "all"
	CredentialTypeClient CredentialType = "client"
	CredentialTypeTokens CredentialType = "tokens"
)

type ClientMetadata struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

type McpOAuthProvider struct {
	mcpName    string
	serverURL  string
	config     OAuthConfig
	store      *AuthStore
	onRedirect func(authURL *url.URL) error
}

func NewMcpOAuthProvider(mcpName, serverURL string, config OAuthConfig, store *AuthStore, onRedirect func(authURL *url.URL) error) *McpOAuthProvider {
	return &McpOAuthProvider{
		mcpName:    mcpName,
		serverURL:  serverURL,
		config:     config,
		store:      store,
		onRedirect: onRedirect,
	}
}

func (p *McpOAuthProvider) RedirectURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", OAuthCallbackPort, OAuthCallbackPath)
}

func (p *McpOAuthProvider) ClientMetadata() ClientMetadata {
	authMethod := "none"
	if p.config.ClientSecret != "" {
		authMethod = "client_secret_post"
	}
	return ClientMetadata{
		RedirectURIs:            []string{p.RedirectURL()},
		ClientName:              "Morpheus",
		ClientURI:               "https://github.com/zetatez/morpheus",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: authMethod,
	}
}

func (p *McpOAuthProvider) ClientInformation(ctx context.Context) (*ClientInfo, error) {
	if p.config.ClientID != "" {
		return &ClientInfo{
			ClientID:     p.config.ClientID,
			ClientSecret: p.config.ClientSecret,
		}, nil
	}

	entry, ok := p.store.GetForURL(p.mcpName, p.serverURL)
	if !ok || entry.ClientInfo == nil {
		return nil, nil
	}

	if entry.ClientInfo.ClientSecretExpiresAt > 0 && entry.ClientInfo.ClientSecretExpiresAt < float64(time.Now().Unix()) {
		return nil, nil
	}

	return entry.ClientInfo, nil
}

func (p *McpOAuthProvider) SaveClientInformation(ctx context.Context, info *ClientInfo) error {
	return p.store.UpdateClientInfo(p.mcpName, info, p.serverURL)
}

func (p *McpOAuthProvider) Tokens(ctx context.Context) (*Tokens, error) {
	entry, ok := p.store.GetForURL(p.mcpName, p.serverURL)
	if !ok || entry.Tokens == nil {
		return nil, nil
	}
	return entry.Tokens, nil
}

func (p *McpOAuthProvider) AccessToken(ctx context.Context) (string, error) {
	tokens, err := p.Tokens(ctx)
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", nil
	}
	return tokens.AccessToken, nil
}

func (p *McpOAuthProvider) SaveTokens(ctx context.Context, tokens *Tokens) error {
	return p.store.UpdateTokens(p.mcpName, tokens, p.serverURL)
}

func (p *McpOAuthProvider) RedirectToAuthorization(ctx context.Context, authURL *url.URL) error {
	if p.onRedirect != nil {
		return p.onRedirect(authURL)
	}
	return nil
}

func (p *McpOAuthProvider) SaveCodeVerifier(ctx context.Context, verifier string) error {
	return p.store.UpdateCodeVerifier(p.mcpName, verifier)
}

func (p *McpOAuthProvider) CodeVerifier(ctx context.Context) (string, error) {
	entry, ok := p.store.Get(p.mcpName)
	if !ok || entry.CodeVerifier == "" {
		return "", fmt.Errorf("no code verifier saved for MCP server: %s", p.mcpName)
	}
	return entry.CodeVerifier, nil
}

func (p *McpOAuthProvider) SaveState(ctx context.Context, state string) error {
	return p.store.UpdateOAuthState(p.mcpName, state)
}

func (p *McpOAuthProvider) State(ctx context.Context) (string, error) {
	entry, ok := p.store.Get(p.mcpName)
	if ok && entry.OAuthState != "" {
		return entry.OAuthState, nil
	}

	newState, err := generateRandomState()
	if err != nil {
		return "", err
	}
	if err := p.store.UpdateOAuthState(p.mcpName, newState); err != nil {
		return "", err
	}
	return newState, nil
}

func (p *McpOAuthProvider) InvalidateCredentials(ctx context.Context, typ CredentialType) error {
	switch typ {
	case CredentialTypeAll:
		return p.store.Remove(p.mcpName)
	case CredentialTypeClient:
		entry, ok := p.store.Get(p.mcpName)
		if !ok {
			return nil
		}
		entry.ClientInfo = nil
		return p.store.Set(p.mcpName, entry)
	case CredentialTypeTokens:
		entry, ok := p.store.Get(p.mcpName)
		if !ok {
			return nil
		}
		entry.Tokens = nil
		return p.store.Set(p.mcpName, entry)
	default:
		return fmt.Errorf("unknown credential type: %s", typ)
	}
}

func (p *McpOAuthProvider) RefreshAccessToken(ctx context.Context, refreshToken string) (*Tokens, error) {
	if p.config.TokenURL == "" {
		return nil, fmt.Errorf("token URL not configured")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	if p.config.ClientID != "" {
		data.Set("client_id", p.config.ClientID)
	}
	if p.config.ClientSecret != "" {
		data.Set("client_secret", p.config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.config.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	tokens := &Tokens{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		Scope:        result.Scope,
	}
	if result.ExpiresIn > 0 {
		tokens.ExpiresAt = float64(time.Now().Unix()) + float64(result.ExpiresIn)
	}

	return tokens, nil
}

func (p *McpOAuthProvider) StartAuthorization(ctx context.Context, authURL string) error {
	if p.config.AuthURL == "" {
		return fmt.Errorf("authorization URL not configured")
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return err
	}
	if err := p.SaveCodeVerifier(ctx, codeVerifier); err != nil {
		return err
	}

	state, err := p.State(ctx)
	if err != nil {
		return err
	}

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", p.config.ClientID)
	params.Set("redirect_uri", p.RedirectURL())
	params.Set("scope", p.config.Scope)
	params.Set("state", state)
	params.Set("code_challenge", codeVerifier)
	params.Set("code_challenge_method", "S256")

	authURLWithParams := authURL + "?" + params.Encode()
	parsedURL, err := url.Parse(authURLWithParams)
	if err != nil {
		return err
	}

	return p.RedirectToAuthorization(ctx, parsedURL)
}

func generateCodeVerifier() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

func generateRandomState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	result := make([]byte, len(bytes)*2)
	for i, b := range bytes {
		result[i*2] = b >> 4
		result[i*2+1] = b & 0x0f
	}
	hex := fmt.Sprintf("%x", result)
	return hex, nil
}

func PKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
