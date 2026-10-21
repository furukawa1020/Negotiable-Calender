package calendar

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// Application guard on an already byte-bounded provider body. Count every
// container, including unknown fields, before encoding/json can apply last-wins
// semantics to security-relevant nested event or error fields.
const providerJSONDepth = 64

func validateProviderJSON(ctx context.Context, data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return errCalendarResponse
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := providerJSONValue(ctx, d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errCalendarResponse
	}
	return ctx.Err()
}

func providerJSONValue(ctx context.Context, d *json.Decoder, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	token, err := d.Token()
	if err != nil {
		return errCalendarResponse
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	if depth >= providerJSONDepth || (delim != '{' && delim != '[') {
		return errCalendarResponse
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok {
				return errCalendarResponse
			}
			folded := foldTokenKey(name)
			if seen[folded] {
				return errCalendarResponse
			}
			seen[folded] = true
		}
		if err := providerJSONValue(ctx, d, depth+1); err != nil {
			return err
		}
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return errCalendarResponse
	}
	return ctx.Err()
}
