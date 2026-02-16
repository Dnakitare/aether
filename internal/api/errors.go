// Package api provides structured error responses following RFC 7807.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ProblemDetail represents an RFC 7807 Problem Details response.
// https://datatracker.ietf.org/doc/html/rfc7807
type ProblemDetail struct {
	// Type is a URI reference that identifies the problem type
	Type string `json:"type"`

	// Title is a short, human-readable summary of the problem
	Title string `json:"title"`

	// Status is the HTTP status code
	Status int `json:"status"`

	// Detail is a human-readable explanation specific to this occurrence
	Detail string `json:"detail,omitempty"`

	// Instance is a URI reference that identifies the specific occurrence
	Instance string `json:"instance,omitempty"`

	// Extensions for additional problem-specific information
	Errors     []FieldError `json:"errors,omitempty"`
	Suggestion string       `json:"suggestion,omitempty"`
	Code       string       `json:"code,omitempty"`
}

// FieldError represents a validation error for a specific field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// Error code constants
const (
	ErrCodeValidation     = "VALIDATION_ERROR"
	ErrCodeNotFound       = "NOT_FOUND"
	ErrCodeUnauthorized   = "UNAUTHORIZED"
	ErrCodeForbidden      = "FORBIDDEN"
	ErrCodeConflict       = "CONFLICT"
	ErrCodeQuotaExceeded  = "QUOTA_EXCEEDED"
	ErrCodeInternalError  = "INTERNAL_ERROR"
	ErrCodeBadRequest     = "BAD_REQUEST"
	ErrCodeServiceUnavail = "SERVICE_UNAVAILABLE"
	ErrCodeTimeout        = "TIMEOUT"
)

// Common problem type URIs
const (
	TypeValidationError = "https://aether.io/problems/validation-error"
	TypeNotFound        = "https://aether.io/problems/not-found"
	TypeUnauthorized    = "https://aether.io/problems/unauthorized"
	TypeForbidden       = "https://aether.io/problems/forbidden"
	TypeConflict        = "https://aether.io/problems/conflict"
	TypeQuotaExceeded   = "https://aether.io/problems/quota-exceeded"
	TypeInternalError   = "https://aether.io/problems/internal-error"
	TypeBadRequest      = "https://aether.io/problems/bad-request"
	TypeServiceUnavail  = "https://aether.io/problems/service-unavailable"
	TypeTimeout         = "https://aether.io/problems/timeout"
)

// NewProblemDetail creates a new problem detail.
func NewProblemDetail(status int, title, detail string) *ProblemDetail {
	pd := &ProblemDetail{
		Status: status,
		Title:  title,
		Detail: detail,
	}

	// Set type and code based on status
	switch status {
	case http.StatusBadRequest:
		pd.Type = TypeBadRequest
		pd.Code = ErrCodeBadRequest
	case http.StatusUnauthorized:
		pd.Type = TypeUnauthorized
		pd.Code = ErrCodeUnauthorized
	case http.StatusForbidden:
		pd.Type = TypeForbidden
		pd.Code = ErrCodeForbidden
	case http.StatusNotFound:
		pd.Type = TypeNotFound
		pd.Code = ErrCodeNotFound
	case http.StatusConflict:
		pd.Type = TypeConflict
		pd.Code = ErrCodeConflict
	case http.StatusInternalServerError:
		pd.Type = TypeInternalError
		pd.Code = ErrCodeInternalError
	case http.StatusServiceUnavailable:
		pd.Type = TypeServiceUnavail
		pd.Code = ErrCodeServiceUnavail
	case http.StatusGatewayTimeout:
		pd.Type = TypeTimeout
		pd.Code = ErrCodeTimeout
	default:
		pd.Type = "about:blank"
	}

	return pd
}

// NewValidationError creates a validation error problem detail.
func NewValidationError(detail string, fieldErrors []FieldError) *ProblemDetail {
	return &ProblemDetail{
		Type:   TypeValidationError,
		Title:  "Validation Failed",
		Status: http.StatusBadRequest,
		Detail: detail,
		Code:   ErrCodeValidation,
		Errors: fieldErrors,
	}
}

// NewNotFoundError creates a not found error.
func NewNotFoundError(resourceType, resourceID string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeNotFound,
		Title:      "Resource Not Found",
		Status:     http.StatusNotFound,
		Detail:     fmt.Sprintf("%s with ID '%s' not found", resourceType, resourceID),
		Code:       ErrCodeNotFound,
		Suggestion: fmt.Sprintf("Check if the %s ID is correct and the resource exists", resourceType),
	}
}

// NewUnauthorizedError creates an unauthorized error.
func NewUnauthorizedError(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeUnauthorized,
		Title:      "Authentication Required",
		Status:     http.StatusUnauthorized,
		Detail:     detail,
		Code:       ErrCodeUnauthorized,
		Suggestion: "Include a valid authentication token in the Authorization header",
	}
}

// NewForbiddenError creates a forbidden error.
func NewForbiddenError(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeForbidden,
		Title:      "Access Forbidden",
		Status:     http.StatusForbidden,
		Detail:     detail,
		Code:       ErrCodeForbidden,
		Suggestion: "Check if you have the required permissions for this operation",
	}
}

// NewConflictError creates a conflict error.
func NewConflictError(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeConflict,
		Title:      "Resource Conflict",
		Status:     http.StatusConflict,
		Detail:     detail,
		Code:       ErrCodeConflict,
		Suggestion: "The resource already exists or is in an incompatible state",
	}
}

// NewQuotaExceededError creates a quota exceeded error.
func NewQuotaExceededError(quotaType string, limit, current int64) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeQuotaExceeded,
		Title:      "Quota Exceeded",
		Status:     http.StatusForbidden,
		Detail:     fmt.Sprintf("%s quota exceeded: limit=%d, current=%d", quotaType, limit, current),
		Code:       ErrCodeQuotaExceeded,
		Suggestion: fmt.Sprintf("Upgrade your plan or reduce %s usage to proceed", quotaType),
	}
}

// NewInternalError creates an internal server error.
func NewInternalError(detail string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeInternalError,
		Title:      "Internal Server Error",
		Status:     http.StatusInternalServerError,
		Detail:     detail,
		Code:       ErrCodeInternalError,
		Suggestion: "This is an unexpected error. Please contact support if it persists",
	}
}

// NewServiceUnavailableError creates a service unavailable error.
func NewServiceUnavailableError(service string) *ProblemDetail {
	return &ProblemDetail{
		Type:       TypeServiceUnavail,
		Title:      "Service Unavailable",
		Status:     http.StatusServiceUnavailable,
		Detail:     fmt.Sprintf("%s is not available", service),
		Code:       ErrCodeServiceUnavail,
		Suggestion: "The requested service is currently unavailable. Please try again later",
	}
}

// WithSuggestion adds a suggestion to the problem detail.
func (pd *ProblemDetail) WithSuggestion(suggestion string) *ProblemDetail {
	pd.Suggestion = suggestion
	return pd
}

// WithInstance adds an instance URI to the problem detail.
func (pd *ProblemDetail) WithInstance(instance string) *ProblemDetail {
	pd.Instance = instance
	return pd
}

// WithFieldErrors adds field errors to the problem detail.
func (pd *ProblemDetail) WithFieldErrors(errors []FieldError) *ProblemDetail {
	pd.Errors = errors
	return pd
}

// respondProblem writes a problem detail response.
func (s *Server) respondProblem(w http.ResponseWriter, pd *ProblemDetail) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(pd.Status)

	if err := json.NewEncoder(w).Encode(pd); err != nil {
		s.logger.Error("failed to encode problem detail", "error", err)
	}
}

// Helper functions for common error responses

// respondValidationError responds with a validation error.
func (s *Server) respondValidationError(w http.ResponseWriter, detail string, fieldErrors []FieldError) {
	s.respondProblem(w, NewValidationError(detail, fieldErrors))
}

// respondNotFound responds with a not found error.
func (s *Server) respondNotFound(w http.ResponseWriter, resourceType, resourceID string) {
	s.respondProblem(w, NewNotFoundError(resourceType, resourceID))
}

// respondUnauthorized responds with an unauthorized error.
func (s *Server) respondUnauthorized(w http.ResponseWriter, detail string) {
	s.respondProblem(w, NewUnauthorizedError(detail))
}

// respondForbidden responds with a forbidden error.
func (s *Server) respondForbidden(w http.ResponseWriter, detail string) {
	s.respondProblem(w, NewForbiddenError(detail))
}

// respondConflict responds with a conflict error.
func (s *Server) respondConflict(w http.ResponseWriter, detail string) {
	s.respondProblem(w, NewConflictError(detail))
}

// respondQuotaExceeded responds with a quota exceeded error.
func (s *Server) respondQuotaExceeded(w http.ResponseWriter, quotaType string, limit, current int64) {
	s.respondProblem(w, NewQuotaExceededError(quotaType, limit, current))
}

// respondInternalError responds with an internal server error.
func (s *Server) respondInternalError(w http.ResponseWriter, detail string) {
	s.respondProblem(w, NewInternalError(detail))
}

// respondServiceUnavailable responds with a service unavailable error.
func (s *Server) respondServiceUnavailable(w http.ResponseWriter, service string) {
	s.respondProblem(w, NewServiceUnavailableError(service))
}
