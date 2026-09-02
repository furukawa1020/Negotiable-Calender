package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CalendarReadonlyScope = "https://www.googleapis.com/auth/calendar.readonly"

type Provider interface {
	Configured() bool
	AuthorizationURL(state, challenge string) string
	Exchange(context.Context, string, string) (TokenSet, error)
	Refresh(context.Context, string) (TokenSet, error)
	ListBusy(context.Context, string, time.Time, time.Time) ([]BusySpan, error)
}

type GoogleConfig struct{ ClientID, ClientSecret, RedirectURL string }

type GoogleProvider struct {
	config                       GoogleConfig
	client                       *http.Client
	authURL, tokenURL, eventsURL string
}

func NewGoogleProvider(config GoogleConfig, client *http.Client) *GoogleProvider {
	return &GoogleProvider{config: config, client: client,
		authURL:   "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:  "https://oauth2.googleapis.com/token",
		eventsURL: "https://www.googleapis.com/calendar/v3/calendars/primary/events"}
}

func (provider *GoogleProvider) Configured() bool {
	return provider.config.ClientID != "" && provider.config.RedirectURL != ""
}

func (provider *GoogleProvider) AuthorizationURL(state, challenge string) string {
	query := url.Values{"client_id": {provider.config.ClientID}, "redirect_uri": {provider.config.RedirectURL},
		"response_type": {"code"}, "scope": {CalendarReadonlyScope}, "state": {state},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "access_type": {"offline"},
		"prompt": {"consent"}, "include_granted_scopes": {"true"}}
	return provider.authURL + "?" + query.Encode()
}

func (provider *GoogleProvider) Exchange(ctx context.Context, code, verifier string) (TokenSet, error) {
	return provider.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {provider.config.RedirectURL}, "code_verifier": {verifier}})
}

func (provider *GoogleProvider) Refresh(ctx context.Context, refreshToken string) (TokenSet, error) {
	return provider.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

func (provider *GoogleProvider) token(ctx context.Context, form url.Values) (TokenSet, error) {
	form.Set("client_id", provider.config.ClientID)
	if provider.config.ClientSecret != "" {
		form.Set("client_secret", provider.config.ClientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenSet{}, fmt.Errorf("create token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := provider.client.Do(request)
	if err != nil {
		return TokenSet{}, fmt.Errorf("exchange calendar token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TokenSet{}, fmt.Errorf("calendar token endpoint returned %s", response.Status)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return TokenSet{}, fmt.Errorf("decode calendar token: %w", err)
	}
	if body.AccessToken == "" {
		return TokenSet{}, fmt.Errorf("calendar token response missing access token")
	}
	return TokenSet{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken, Scopes: strings.Fields(body.Scope), ExpiresAt: time.Now().UTC().Add(time.Duration(body.ExpiresIn) * time.Second)}, nil
}

func (provider *GoogleProvider) ListBusy(ctx context.Context, accessToken string, from, to time.Time) ([]BusySpan, error) {
	query := url.Values{"timeMin": {from.Format(time.RFC3339)}, "timeMax": {to.Format(time.RFC3339)},
		"singleEvents": {"true"}, "showDeleted": {"false"}, "maxResults": {"2500"},
		"fields": {"items(id,start,end,transparency,status),nextPageToken"}}
	next := ""
	result := []BusySpan{}
	for {
		if next != "" {
			query.Set("pageToken", next)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.eventsURL+"?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("create events request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+accessToken)
		response, err := provider.client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("list calendar events: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("calendar events endpoint returned %s", response.Status)
		}
		var body struct {
			NextPageToken string `json:"nextPageToken"`
			Items         []struct {
				ID, Transparency, Status string
				Start, End               struct{ DateTime, Date string }
			} `json:"items"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode calendar events: %w", err)
		}
		for _, item := range body.Items {
			if item.Status == "cancelled" {
				continue
			}
			start, err := googleTime(item.Start.DateTime, item.Start.Date)
			if err != nil {
				continue
			}
			end, err := googleTime(item.End.DateTime, item.End.Date)
			if err != nil || !end.After(start) {
				continue
			}
			result = append(result, BusySpan{ProviderEventID: item.ID, CalendarID: "primary", StartAt: start, EndAt: end, Busy: item.Transparency != "transparent"})
		}
		if body.NextPageToken == "" {
			return result, nil
		}
		next = body.NextPageToken
	}
}

func googleTime(dateTime, date string) (time.Time, error) {
	if dateTime != "" {
		value, err := time.Parse(time.RFC3339, dateTime)
		return value.UTC(), err
	}
	value, err := time.Parse("2006-01-02", date)
	return value.UTC(), err
}
