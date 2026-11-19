package service

import (
	"github.com/kiflet/backend/internal/platform/apperror"
)

// The errors a person can act on are internal/platform/apperror's, shared
// with the modules.
type (
	Error = apperror.Error
	Kind  = apperror.Kind
)

const (
	KindInvalid      = apperror.KindInvalid
	KindNotFound     = apperror.KindNotFound
	KindConflict     = apperror.KindConflict
	KindForbidden    = apperror.KindForbidden
	KindUnauthorized = apperror.KindUnauthorized
	KindLimit        = apperror.KindLimit
)

var (
	invalid      = apperror.Invalid
	notFound     = apperror.NotFound
	conflict     = apperror.Conflict
	forbidden    = apperror.Forbidden
	limitReached = apperror.Limit
	// AsError extracts a domain error, if err is one.
	AsError = apperror.As
	// IsNotFound reports whether err is a not-found domain error.
	IsNotFound = apperror.IsNotFound
)

var (
	ErrAccountLocked      = &Error{Kind: KindForbidden, Message: "Too many failed attempts. The account is locked for 15 minutes."}
	ErrInvalidCredentials = &Error{Kind: KindUnauthorized, Message: "Incorrect login details."}
	ErrAccountInactive    = &Error{Kind: KindForbidden, Message: "This account has been deactivated. Contact your administrator."}
	ErrSessionRevoked     = &Error{Kind: KindUnauthorized, Message: "Your session has ended. Please sign in again."}
	ErrWrongApp           = &Error{Kind: KindForbidden, Message: "Drivers sign in through the driver app."}
	ErrNotDriver          = &Error{Kind: KindForbidden, Message: "Only drivers can use the driver app."}
	ErrSamePassword       = &Error{Kind: KindInvalid, Field: "new_password", Message: "Choose a password different from the current one."}
	ErrUnauthorized       = &Error{Kind: KindForbidden, Message: "You do not have permission to do that."}
	errAnonymised         = &Error{Kind: KindConflict, Message: "This person's details were removed, so the account can't be used again. Add them as a new person instead."}
	errShipmentAnonymised = &Error{Kind: KindConflict, Message: "This shipment's customer and delivery details were removed after the keeping period, so it can't be changed. Book a new shipment instead."}
	ErrNotAssigned        = &Error{Kind: KindForbidden, Message: "This shipment is not assigned to you."}
	ErrStaleUpdate        = &Error{Kind: KindConflict, Message: "The shipment was changed while you were offline, so this update was not applied."}
)

// suspended is the error for a suspended company's users: they can't sign in
// to ask on the Help page, so it says where to write.
func (c *core) suspended() error {
	return &Error{Kind: KindForbidden, Message: "This company account is suspended. Write to " + c.cfg.SupportEmail + " to have it restored."}
}
