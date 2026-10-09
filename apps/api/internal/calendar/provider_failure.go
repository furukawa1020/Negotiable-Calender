package calendar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

const googleErrorBodyBytes = 16 * 1024

// Only allowlisted classifications leave this function. Never retain provider
// messages/descriptions: they may contain identifiers or token material.
// The caller owns closing response.Body on every path.
func googleFailure(ctx context.Context, response *http.Response, token bool, service string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	failure := providerStatusError{service: service, status: response.StatusCode}
	if response.StatusCode == http.StatusTooManyRequests {
		failure.category = "rate_limited"
		return failure
	}
	if !token && response.StatusCode == http.StatusUnauthorized {
		return ErrReconnectRequired
	}
	if !token && response.StatusCode == http.StatusForbidden {
		failure.category = "provider_denied"
	}
	if (token && response.StatusCode != 400 && response.StatusCode != 401) || (!token && response.StatusCode != 403) {
		return failure
	}
	if response.ContentLength > googleErrorBodyBytes {
		return failure
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, googleErrorBodyBytes+1))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || len(data) > googleErrorBodyBytes {
		return failure
	}
	if token {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &body) != nil {
			return failure
		}
		switch body.Error {
		case "invalid_grant":
			failure.category = "invalid_grant"
		case "invalid_client", "unauthorized_client":
			failure.category = "provider_configuration"
		}
		return failure
	}
	var body struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil || len(body.Error.Errors) == 0 {
		return failure
	}
	category := ""
	for _, entry := range body.Error.Errors {
		current := ""
		switch entry.Reason {
		case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
			current = "rate_limited"
		case "insufficientPermissions":
			current = "reconnect_required"
		default:
			return failure
		}
		if category != "" && category != current {
			return failure
		}
		category = current
	}
	if category == "reconnect_required" {
		return ErrReconnectRequired
	}
	failure.category = category
	return failure
}
