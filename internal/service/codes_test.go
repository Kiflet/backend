package service

import (
	"slices"
	"testing"

	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/modules/driversync"
	identity "github.com/kiflet/backend/internal/modules/identity/domain"
	"github.com/kiflet/backend/internal/platform/text"
)

// TestEveryCodeHasWords (audit D8): every code a person reads has words for
// each audience that reads it, in English and in Amharic, in one catalogue
// (internal/platform/text). A new status, reason, alert or state fails here
// until it has them.
func TestEveryCodeHasWords(t *testing.T) {
	reasons := slices.Concat(model.DelayReasons, model.IssueReasons, model.CancelReasons, model.LocationReasons)
	// A driver's buttons: the statuses they may move a load to, the
	// delivery, and going back on the road after a delay.
	moves := []string{model.StatusDelivered, "resume"}
	for _, s := range model.AllStatuses {
		for _, next := range model.AllowedTransitions(s, model.ActorDriver) {
			if !slices.Contains(moves, next) {
				moves = append(moves, next)
			}
		}
	}
	held := []string{HeldCancelled, HeldReassigned, HeldUnassigned, HeldDuplicate, "changed"}
	roles := []string{identity.RoleDriver, identity.RoleDispatcher, identity.RoleFleetManager, identity.RoleAdmin, identity.RolePlatformAdmin}
	for _, set := range []struct {
		audience, kind string
		codes          []string
	}{
		{text.Office, text.KindStatus, model.AllStatuses},
		{text.Customer, text.KindStatus, model.AllStatuses},
		{text.Driver, text.KindStatus, model.AllStatuses},
		{text.Office, text.KindReason, reasons},
		{text.Customer, text.KindReason, slices.Concat(model.DelayReasons, model.IssueReasons)},
		{text.Driver, text.KindReason, slices.Concat(model.DelayReasons, model.IssueReasons, model.LocationReasons)},
		{text.Office, text.KindRole, roles},
		{text.Office, text.KindAlert, AlertTypes},
		{text.Office, text.KindHeld, held},
		{text.Driver, text.KindHeld, held},
		{text.Office, text.KindInvoice, InvoiceStates},
		{text.Office, text.KindPayment, PaymentStates},
		{text.Driver, text.KindMove, moves},
		{text.Driver, text.KindRefusal, driversync.Refusals},
		{text.Office, text.KindPriority, model.Priorities},
	} {
		for _, code := range set.codes {
			for _, l := range text.Languages {
				if !text.HasLabel(l.Code, set.audience, set.kind, code) {
					t.Errorf("%s %s %q has no words for the %s", l.Name, set.kind, code, set.audience)
				}
			}
		}
	}
}
