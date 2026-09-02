package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const CalendarReadonlyScope = "https://www.googleapis.com/auth/calendar.readonly"

var (
	ErrReconnectRequired = errors.New("calendar reconnect required")
	ErrSyncTokenExpired   = errors.New("calendar sync token expired")
)

type Provider interface {
	Configured() bool
	AuthorizationURL(state, challenge string) string
	Exchange(context.Context, string, string) (TokenSet, error)
	Refresh(context.Context, string) (TokenSet, error)
	ListBusy(context.Context, string, time.Time, time.Time) ([]BusySpan, error)
}

type IncrementalProvider interface {
	ListChanges(context.Context, string, string, time.Time, time.Time) (ChangeSet, error)
}

type GoogleConfig struct{ ClientID, ClientSecret, RedirectURL string }

type GoogleProvider struct {
	config                       GoogleConfig
	client                       *http.Client
	authURL, tokenURL, eventsURL string
}

type providerStatusError struct {
	service string
	status  int
}

func (value providerStatusError) Error() string {
	return fmt.Sprintf("%s endpoint returned %d", value.service, value.status)
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
	tokens, err := provider.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	var statusErr providerStatusError
	if errors.As(err, &statusErr) && (statusErr.status == http.StatusBadRequest || statusErr.status == http.StatusUnauthorized) {
		return TokenSet{}, fmt.Errorf("%w: token refresh rejected", ErrReconnectRequired)
	}
	return tokens, err
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
		return TokenSet{}, providerStatusError{service: "calendar token", status: response.StatusCode}
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
	changes, err := provider.ListChanges(ctx, accessToken, "", from, to)
	return changes.Upserts, err
}

func (provider *GoogleProvider) ListChanges(ctx context.Context, accessToken, syncToken string, from, to time.Time) (ChangeSet, error) {
	query := url.Values{
		"singleEvents": {"true"}, "showDeleted": {"true"}, "maxResults": {"2500"},
		"fields": {"items(id,start,end,transparency,status),nextPageToken,nextSyncToken"},
	}
	full := syncToken == ""
	if full {
		query.Set("timeMin", from.Format(time.RFC3339))
		query.Set("timeMax", to.Format(time.RFC3339))
	} else {
		query.Set("syncToken", syncToken)
	}

	nextPage := ""
	result := ChangeSet{Full: full, Upserts: []BusySpan{}, DeletedProviderEventIDs: []string{}}
	for {
		if nextPage != "" {
			query.Set("pageToken", nextPage)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.eventsURL+"?"+query.Encode(), nil)
		if err != nil {
			return ChangeSet{}, fmt.Errorf("create events request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+accessToken)
		response, err := provider.client.Do(request)
		if err != nil {
			return ChangeSet{}, fmt.Errorf("list calendar events: %w", err)
		}
		if response.StatusCode == http.StatusGone && !full {
			response.Body.Close()
			return ChangeSet{}, ErrSyncTokenExpired
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			response.Body.Close()
			return ChangeSet{}, ErrReconnectRequired
		}
		if response.StatusCode != http.StatusOK {
			status := response.StatusCode
			response.Body.Close()
			return ChangeSet{}, providerStatusError{service: "calendar events", status: status}
		}
		var body struct {
			NextPageToken string `json:"nextPageToken"`
			NextSyncToken string `json:"nextSyncToken"`
			Items         []struct {
				ID, Transparency, Status string
				Start, End               struct{ DateTime, Date string }
			} `json:"items"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			return ChangeSet{}, fmt.Errorf("decode calendar events: %w", err)
		}
		for _, item := range body.Items {
			if item.ID == "" {
				continue
			}
			if item.Status == "cancelled" {
				if !full {
					result.DeletedProviderEventIDs = append(result.DeletedProviderEventIDs, item.ID)
				}
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
			result.Upserts = append(result.Upserts, BusySpan{ProviderEventID: item.ID, CalendarID: "primary", StartAt: start, EndAt: end, Busy: item.Transparency != "transparent"})
		}
		if body.NextPageToken == "" {
			if body.NextSyncToken == "" {
				return ChangeSet{}, fmt.Errorf("calendar events response missing next sync token")
			}
			result.NextSyncToken = body.NextSyncToken
			return result, nil
		}
		nextPage = body.NextPageToken
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


func (provider *GoogleProvider) ListPrivateEvents(ctx context.Context, accessToken string, from, to time.Time) ([]PrivateEventView, error) {
	query := url.Values{
		"timeMin": {from.UTC().Format(time.RFC3339)}, "timeMax": {to.UTC().Format(time.RFC3339)},
		"singleEvents": {"true"}, "showDeleted": {"false"}, "maxResults": {"250"},
		"fields": {"items(id,summary,description,location,attendees(displayName,email,self),start,end,status,htmlLink,hangoutLink),nextPageToken"},
	}
	nextPage := ""
	result := []PrivateEventView{}
	for {
		if nextPage != "" {
			query.Set("pageToken", nextPage)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.eventsURL+"?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("create private events request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+accessToken)
		response, err := provider.client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("list private calendar events: %w", err)
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			response.Body.Close()
			return nil, ErrReconnectRequired
		}
		if response.StatusCode != http.StatusOK {
			status := response.StatusCode
			response.Body.Close()
			return nil, providerStatusError{service: "private calendar events", status: status}
		}
		var body struct {
			NextPageToken string `json:"nextPageToken"`
			Items []struct {
				ID, Summary, Description, Location, Status, HTMLLink, HangoutLink string
				Attendees []struct {
					DisplayName string `json:"displayName"`
					Email       string `json:"email"`
					Self        bool   `json:"self"`
				} `json:"attendees"`
				Start, End struct {
					DateTime string `json:"dateTime"`
					Date     string `json:"date"`
				}
			} `json:"items"`
		}
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode private calendar events: %w", err)
		}
		for _, item := range body.Items {
			if item.Status == "cancelled" || item.ID == "" {
				continue
			}
			value := PrivateEventView{
				ID: item.ID, Title: item.Summary, Description: item.Description,
				Location: item.Location, ConferenceURL: item.HangoutLink,
				Attendees: []string{},
			}
			if value.Title == "" {
				value.Title = "(タイトルなし)"
			}
			for _, attendee := range item.Attendees {
				if attendee.Self {
					continue
				}
				label := attendee.DisplayName
				if label == "" {
					label = attendee.Email
				}
				if label != "" {
					value.Attendees = append(value.Attendees, label)
				}
			}
			if item.Start.Date != "" {
				value.AllDay, value.StartDate, value.EndDate = true, item.Start.Date, item.End.Date
			} else {
				start, startErr := googleTime(item.Start.DateTime, "")
				end, endErr := googleTime(item.End.DateTime, "")
				if startErr != nil || endErr != nil || !end.After(start) {
					continue
				}
				value.StartAt, value.EndAt = &start, &end
			}
			result = append(result, value)
			if len(result) > 1000 {
				return nil, fmt.Errorf("private calendar event limit exceeded")
			}
		}
		if body.NextPageToken == "" {
			return result, nil
		}
		nextPage = body.NextPageToken
	}
}
