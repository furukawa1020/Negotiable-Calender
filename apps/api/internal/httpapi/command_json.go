package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Decode the whole bounded command before any side effect. A JSON null is not
// an empty command, and a valid prefix must not hide a second value or garbage.
func decodeCommand[T any](response http.ResponseWriter, request *http.Request, target *T, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, limit))
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
