package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const (
	defaultHost     = "github.com"
	deviceCodePath  = "/login/device/code"
	accessTokenPath = "/login/oauth/access_token"
	tokenScope      = "repo"

	keyringService = "platformr"
	keyringUser    = "github-app-token"

	// fallbackTokenFile is used only when no keychain is available (headless/CI).
	fallbackTokenFile = ".config/platformr/token"
)

// LoadToken returns the stored app OAuth token.
// Checks the OS keychain first, falls back to the file on disk.
func LoadToken() string {
	// Keychain
	t, err := keyring.Get(keyringService, keyringUser)
	if err == nil && t != "" {
		return t
	}

	// File fallback (headless environments, CI)
	if data, err := os.ReadFile(fallbackPath()); err == nil {
		return strings.TrimSpace(string(data))
	}

	return ""
}

// SaveToken stores the app OAuth token.
// Uses the OS keychain when available, falls back to a 0600 file.
func SaveToken(token string) error {
	err := keyring.Set(keyringService, keyringUser, token)
	if err == nil {
		return nil
	}

	// Keychain unavailable — fall back to file with restricted permissions
	path := fallbackPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0o600)
}

// ClearToken removes the stored token from the keychain and/or file.
func ClearToken() error {
	kerr := keyring.Delete(keyringService, keyringUser)
	ferr := os.Remove(fallbackPath())

	// Only error if both failed and the file error wasn't "not found"
	if kerr != nil && !errors.Is(ferr, os.ErrNotExist) {
		return ferr
	}
	return nil
}

// TokenPath returns the fallback file path for display purposes.
func TokenPath() (string, error) {
	return fallbackPath(), nil
}

func fallbackPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return fallbackTokenFile
	}
	return filepath.Join(home, fallbackTokenFile)
}

// DeviceFlowResult holds the codes shown to the user during auth.
type DeviceFlowResult struct {
	UserCode        string
	VerificationURI string
}

type deviceCodeResponse struct {
	DeviceCode       string `json:"device_code"`
	UserCode         string `json:"user_code"`
	VerificationURI  string `json:"verification_uri"`
	ExpiresIn        int    `json:"expires_in"`
	Interval         int    `json:"interval"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
}

// DeviceFlow runs the GitHub App OAuth device flow and returns the access token.
// clientID is the GitHub App's Client ID (public, not a secret).
// host defaults to "github.com" — set to your GHES hostname if needed.
// onCode is called as soon as the user-facing code is available so the CLI
// can display it while polling continues in the background.
func DeviceFlow(clientID, host string, onCode func(DeviceFlowResult)) (string, error) {
	if host == "" {
		host = defaultHost
	}

	dcResp, err := requestDeviceCode(clientID, host)
	if err != nil {
		return "", fmt.Errorf("requesting device code: %w", err)
	}

	if onCode != nil {
		onCode(DeviceFlowResult{
			UserCode:        dcResp.UserCode,
			VerificationURI: dcResp.VerificationURI,
		})
	}

	interval := time.Duration(dcResp.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dcResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		token, err := pollAccessToken(clientID, dcResp.DeviceCode, host)
		if err != nil {
			return "", err
		}
		if token != "" {
			return token, nil
		}
	}

	return "", fmt.Errorf("authorization timed out — run `platformr auth` to try again")
}

func requestDeviceCode(clientID, host string) (*deviceCodeResponse, error) {
	endpoint := fmt.Sprintf("https://%s%s", host, deviceCodePath)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(url.Values{
		"client_id": {clientID},
		"scope":     {tokenScope},
	}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("GitHub error: %s — %s", result.Error, result.ErrorDescription)
	}
	return &result, nil
}

func pollAccessToken(clientID, deviceCode, host string) (string, error) {
	endpoint := fmt.Sprintf("https://%s%s", host, accessTokenPath)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result accessTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	switch result.Error {
	case "":
		return result.AccessToken, nil
	case "authorization_pending", "slow_down":
		return "", nil
	case "expired_token":
		return "", fmt.Errorf("authorization code expired — run `platformr auth` again")
	case "access_denied":
		return "", fmt.Errorf("authorization denied")
	default:
		return "", fmt.Errorf("unexpected error from GitHub: %s", result.Error)
	}
}
