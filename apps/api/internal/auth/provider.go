package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Provider interface {
	AuthorizationURL(state, codeChallenge string) string
	Exchange(context.Context, string, string) (Profile, error)
	Configured() bool
}

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
}

type GoogleProvider struct {
	config GoogleConfig
	client *http.Client
}

func NewGoogleProvider(config GoogleConfig, client *http.Client) *GoogleProvider {
	if config.AuthorizeURL == "" {
		config.AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if config.TokenURL == "" {
		config.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if config.UserInfoURL == "" {
		config.UserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	}
	if client == nil {
		client = &http.Client{}
	}
	return &GoogleProvider{config: config, client: client}
}

func (provider *GoogleProvider) Configured() bool {
	return provider.config.ClientID != "" && provider.config.RedirectURL != ""
}

func (provider *GoogleProvider) AuthorizationURL(state, codeChallenge string) string {
	query := url.Values{
		"client_id":              {provider.config.ClientID},
		"redirect_uri":           {provider.config.RedirectURL},
		"response_type":          {"code"},
		"scope":                  {"openid profile email"},
		"state":                  {state},
		"code_challenge":         {codeChallenge},
		"code_challenge_method":  {"S256"},
		"include_granted_scopes": {"true"},
	}
	return provider.config.AuthorizeURL + "?" + query.Encode()
}

func (provider *GoogleProvider) Exchange(ctx context.Context, code, codeVerifier string) (Profile, error) {
	form := url.Values{
		"client_id":     {provider.config.ClientID},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {provider.config.RedirectURL},
	}
	if provider.config.ClientSecret != "" {
		form.Set("client_secret", provider.config.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Profile{}, fmt.Errorf("create token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := provider.client.Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("token endpoint returned %s", response.Status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&token); err != nil || token.AccessToken == "" {
		return Profile{}, fmt.Errorf("decode token response")
	}

	request, err = http.NewRequestWithContext(ctx, http.MethodGet, provider.config.UserInfoURL, nil)
	if err != nil {
		return Profile{}, fmt.Errorf("create userinfo request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	response, err = provider.client.Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("load userinfo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Profile{}, fmt.Errorf("userinfo endpoint returned %s", response.Status)
	}
	var value struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&value); err != nil {
		return Profile{}, fmt.Errorf("decode userinfo: %w", err)
	}
	if value.Subject == "" || value.Email == "" || !value.EmailVerified {
		return Profile{}, fmt.Errorf("google account email is not verified")
	}
	return Profile{Subject: value.Subject, Email: value.Email, EmailVerified: true, DisplayName: value.Name, AvatarURL: value.Picture}, nil
}
