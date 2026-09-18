package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

// ErrorCategory is a small closed set of coarse failure reasons. Tool errors
// can embed response bodies or file content (see excerpt() in
// internal/server), so telemetry never carries err.Error() — only one of
// these categories, chosen by the error's type, never its message text.
type ErrorCategory string

const (
	CategoryNone         ErrorCategory = ""
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryConvertError ErrorCategory = "convert_error"
	CategoryNetworkError ErrorCategory = "network_error"
	CategoryTimeout      ErrorCategory = "timeout"
	CategoryOther        ErrorCategory = "other"
)

// Categorize maps err to a coarse category. It only looks at error types
// (errors.As/errors.Is), never at formatted message text.
func Categorize(err error) ErrorCategory {
	if err == nil {
		return CategoryNone
	}
	var statusErr *source.StatusError
	if errors.As(err, &statusErr) {
		if statusErr.Code == http.StatusNotFound {
			return CategoryNotFound
		}
		return CategoryNetworkError
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return CategoryTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return CategoryTimeout
		}
		return CategoryNetworkError
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF) {
		return CategoryInvalidInput
	}
	return CategoryOther
}
