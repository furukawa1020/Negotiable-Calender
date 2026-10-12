package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Application safety limit, not a Google token-size guarantee.
const googleTokenResponseBytes = 64 * 1024
const maxTokenLifetimeSeconds = int64((1<<63 - 1) / time.Second)

var errGoogleTokenResponse = errors.New("invalid calendar token response")

// The caller closes the body on every path, including status/parser failures.
// Do not wrap decoder or reader errors: either can contain credential material.
func decodeGoogleToken(ctx context.Context, response *http.Response) (TokenSet, error) {
	if err := ctx.Err(); err != nil {
		return TokenSet{}, err
	}
	if response.ContentLength > googleTokenResponseBytes {
		return TokenSet{}, errGoogleTokenResponse
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, googleTokenResponseBytes+1))
	if ctx.Err() != nil {
		return TokenSet{}, ctx.Err()
	}
	if err != nil || len(data) > googleTokenResponseBytes {
		return TokenSet{}, errGoogleTokenResponse
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' || !json.Valid(data) {
		return TokenSet{}, errGoogleTokenResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return TokenSet{}, errGoogleTokenResponse
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return TokenSet{}, errGoogleTokenResponse
		}
		name := strings.ToLower(key.(string))
		if seen[name] {
			return TokenSet{}, errGoogleTokenResponse
		}
		seen[name] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return TokenSet{}, errGoogleTokenResponse
		}
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(data, &body) != nil || !usableBearerToken(body.AccessToken) ||
		body.ExpiresIn <= 0 || body.ExpiresIn > maxTokenLifetimeSeconds ||
		(body.TokenType != "" && !strings.EqualFold(body.TokenType, "Bearer")) {
		return TokenSet{}, errGoogleTokenResponse
	}
	if err := ctx.Err(); err != nil {
		return TokenSet{}, err
	}
	return TokenSet{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Scopes: strings.Fields(body.Scope), ExpiresAt: time.Now().UTC().Add(time.Duration(body.ExpiresIn) * time.Second)}, nil
}

func usableBearerToken(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c <= ' ' || c >= 127 {
			return false
		}
	}
	return true
}
