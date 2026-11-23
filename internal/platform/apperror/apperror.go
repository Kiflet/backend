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
	// Fields is every invalid field with its message, when there is more
	// than one; Field and Message are then the first of them.
	Fields map[string]string
	// Code, when set, is more precise than the kind's code
	// (idempotency_key_reused, version_mismatch).
	Code string
}

func (e *Error) Error() string { return e.Message }

// APICode is the error's code in the API: its own, or its kind's.
func (e *Error) APICode() string {
	if e.Code != "" {
		return e.Code
	}
	return e.Kind.Code()
}

// FieldMessages is every invalid field with its message, for the API's
// fields; nil when the error names no field.
func (e *Error) FieldMessages() map[string]string {
	if len(e.Fields) > 0 {
		return e.Fields
	}
	if e.Field != "" {
		return map[string]string{e.Field: e.Message}
	}
	return nil
}

// Kind classifies an Error for transport mapping.
type Kind int

const (
	KindInvalid Kind = iota
	KindNotFound
	KindConflict
	KindForbidden
	KindUnauthorized
	KindLimit
	// KindPrecondition is a change made from a copy that is no longer
	// current (If-Match).
	KindPrecondition
	// KindTooLarge is a request larger than the server takes.
	KindTooLarge
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
	case KindPrecondition:
		return http.StatusPreconditionFailed
	case KindTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusBadRequest
	}
}

// Code is the stable machine-readable form of a Kind for API clients, in
// lowercase snake case (docs/architecture.md, section 11, "Conventions").
func (k Kind) Code() string {
	switch k {
	case KindNotFound:
		return "not_found"
	case KindConflict:
		return "conflict"
	case KindForbidden:
		return "forbidden"
	case KindUnauthorized:
		return "unauthorized"
	case KindLimit:
		return "plan_limit"
	case KindPrecondition:
		return "version_mismatch"
	case KindTooLarge:
		return "too_large"
	default:
		return "validation_error"
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
	return errors.AsType[*Error](err)
}

// IsNotFound reports whether err is a not-found domain error.
func IsNotFound(err error) bool {
	e, ok := As(err)
	return ok && e.Kind == KindNotFound
}

// Problems collects every invalid field of an input, so that one answer
// names them all. The zero value is ready to use.
type Problems struct {
	first  *Error
	fields map[string]string
}

// Add records a field's problem; a field keeps its first.
func (p *Problems) Add(field, msg string) {
	if p.first == nil {
		p.first = &Error{Kind: KindInvalid, Field: field, Message: msg}
	}
	if p.fields == nil {
		p.fields = map[string]string{}
	}
	if _, ok := p.fields[field]; !ok {
		p.fields[field] = msg
	}
}

// AddErr records err when it is an invalid input naming a field, and says
// whether it did; any other error is the caller's to return.
func (p *Problems) AddErr(err error) bool {
	e, ok := As(err)
	if !ok || e.Kind != KindInvalid || e.Field == "" {
		return false
	}
	p.Add(e.Field, e.Message)
	for f, m := range e.Fields {
		p.Add(f, m)
	}
	return true
}

// Err is nil when nothing was added, or an invalid-input error whose Field
// and Message are the first problem and whose Fields are all of them.
func (p *Problems) Err() error {
	if p.first == nil {
		return nil
	}
	e := *p.first
	if len(p.fields) > 1 {
		e.Fields = p.fields
	}
	return &e
}
