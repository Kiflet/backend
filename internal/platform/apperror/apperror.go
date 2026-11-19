// Package apperror is the errors a person using the product can act on,
// shared by every module and by the HTTP layer that shows them.
package apperror

import (
	"errors"
	"net/http"
)

// Error is a failure the caller can act on: bad input, a missing record, a
// rule that forbids the action. Its message is written for the person using
// the product and is safe to show them. Anything that is not an *Error is an
// internal fault, whose details belong in the log rather than the response.
type Error struct {
	Kind    Kind
	Message string
	// Field names the input field at fault, when there is one, so a form can
	// show the message next to it.
	Field string
	// Key is the message's key in internal/platform/text, set on the errors a
	// customer can meet, so that their pages can say it in Amharic.
	Key string
}

func (e *Error) Error() string { return e.Message }

// Kind classifies an Error for transport mapping.
type Kind int

const (
	KindInvalid Kind = iota
	KindNotFound
	KindConflict
	KindForbidden
	KindUnauthorized
	KindLimit
)

// HTTPStatus maps a Kind to a status code.
func (k Kind) HTTPStatus() int {
	switch k {
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindForbidden:
		return http.StatusForbidden
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindLimit:
		return http.StatusPaymentRequired
	default:
		return http.StatusBadRequest
	}
}

// Code is the stable machine-readable form of a Kind for API clients.
func (k Kind) Code() string {
	switch k {
	case KindNotFound:
		return "NOT_FOUND"
	case KindConflict:
		return "CONFLICT"
	case KindForbidden:
		return "FORBIDDEN"
	case KindUnauthorized:
		return "UNAUTHORIZED"
	case KindLimit:
		return "PLAN_LIMIT"
	default:
		return "VALIDATION_ERROR"
	}
}

// Invalid is bad input, naming the field at fault when there is one.
func Invalid(field, msg string) error { return &Error{Kind: KindInvalid, Field: field, Message: msg} }

// NotFound is a record that isn't there, or isn't the caller's to see.
func NotFound(msg string) error { return &Error{Kind: KindNotFound, Message: msg} }

// Conflict is a change the record's state doesn't allow.
func Conflict(msg string) error { return &Error{Kind: KindConflict, Message: msg} }

// Forbidden is an action the caller may not take.
func Forbidden(msg string) error { return &Error{Kind: KindForbidden, Message: msg} }

// Limit is a plan's limit reached.
func Limit(msg string) error { return &Error{Kind: KindLimit, Message: msg} }

// As extracts an *Error, if err is one.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsNotFound reports whether err is a not-found domain error.
func IsNotFound(err error) bool {
	e, ok := As(err)
	return ok && e.Kind == KindNotFound
}
