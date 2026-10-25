package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"
)

// Decode the whole bounded command before any side effect. A JSON null is not
// an empty command, and a valid prefix must not hide a second value or garbage.
func decodeCommand[T any](response http.ResponseWriter, request *http.Request, target *T, limit int64) error {
	body, err := io.ReadAll(http.MaxBytesReader(response, request.Body, limit))
	if err != nil {
		return err
	}
	validator := json.NewDecoder(bytes.NewReader(body))
	if err := uniqueCommandKeys(validator, 0); err != nil {
		return err
	}
	if _, err := validator.Token(); err != io.EOF {
		return fmt.Errorf("single command required")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var value *T
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("command object required")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("single command required")
	}
	*target = *value
	return nil
}

// encoding/json accepts repeated fields (including case variants). Reject that
// ambiguity before decoding into a command or allowing any state change.
func uniqueCommandKeys(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("command nesting limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("command object key required")
			}
			name = strings.Map(func(r rune) rune {
				minimum := r
				for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
					if next < minimum {
						minimum = next
					}
				}
				return minimum
			}, name)
			if seen[name] {
				return fmt.Errorf("duplicate command field")
			}
			seen[name] = true
			if err := uniqueCommandKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueCommandKeys(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid command delimiter")
	}
	_, err = decoder.Token()
	return err
}
