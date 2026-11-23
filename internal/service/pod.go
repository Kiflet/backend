package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/modules/driversync"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/internal/platform/phone"
	"github.com/kiflet/backend/internal/platform/text"
	"github.com/kiflet/backend/pkg/storage"
)

// What happened to a proof of delivery, matching the proof_of_deliveries
// check constraint.
const (
	PODAccepted  = "accepted"
	PODHeld      = "held"
	PODDismissed = "dismissed"
)

// Why a proof of delivery was held for the office to review.
const (
	HeldCancelled  = "cancelled"
	HeldReassigned = "reassigned"
	HeldUnassigned = "unassigned"
	HeldDuplicate  = "duplicate"
	// HeldChanged stands for any other: the shipment changed.
	HeldChanged = "changed"
)

// HeldCodes are every reason a proof may be held, for their words.
var HeldCodes = []string{HeldCancelled, HeldReassigned, HeldUnassigned, HeldDuplicate, HeldChanged}

// HeldReasonLabel explains to the office why a proof was held.
func HeldReasonLabel(code string) string {
	return heldWords(text.Office, code)
}

// heldForReview answers a driver whose proof was held: kept, for the
// office to review, with nothing more to send.
func heldForReview(code string) error {
	return &Error{Kind: KindConflict, Code: driversync.RefusedHeld, Message: heldMessage(code)}
}

// heldMessage is what the driver's app says about a held proof.
func heldMessage(code string) string {
	return heldWords(text.Driver, code)
}

// heldWords are an audience's words for why a proof was held, in English;
// a reason without its own words is the shipment having changed.
func heldWords(audience, code string) string {
	if !text.HasLabel(text.English, audience, text.KindHeld, code) {
		code = HeldChanged
	}
	return text.Label(text.English, audience, text.KindHeld, code)
}

// PODInput is a recorded proof of delivery whose media is already stored.
type PODInput struct {
	RecipientName  string
	RecipientPhone string
	Signature      PODFile
	Photos         []PODFile
	Notes          string
	Lat, Lng       *float64
	// LocationReason is why the driver recorded it away from the
	// destination, one of model.LocationReasons.
	LocationReason string
	RecordedAt     time.Time
	ClientID       string
}

// Check normalises the recipient's details and reports what makes the proof
// unacceptable, so a proof that will be refused is refused before its images
// are stored.
func (in *PODInput) Check() error {
	in.RecipientName = strings.TrimSpace(in.RecipientName)
	if in.RecipientName == "" {
		return invalid("recipient_name", "Enter who received the delivery.")
	}
	if utf8.RuneCountInString(in.RecipientName) > 120 {
		return invalid("recipient_name", "That name is too long.")
	}
	if p := strings.TrimSpace(in.RecipientPhone); p != "" {
		n, err := phone.Normalize(p)
		if err != nil {
			return invalid("recipient_phone", err.Error())
		}
		in.RecipientPhone = n
	} else {
		in.RecipientPhone = ""
	}
	// A reason this server doesn't know, from an app newer or older than
	// it, still says the driver had one.
	if r := strings.TrimSpace(in.LocationReason); r != "" && !slices.Contains(model.LocationReasons, r) {
		in.LocationReason = "other"
	} else {
		in.LocationReason = r
	}
	return nil
}

func (in *PODInput) mediaKeys() []string {
	keys := []string{in.Signature.Key}
	for _, p := range in.Photos {
		keys = append(keys, p.Key)
	}
	return keys
}

// ownedKeys are the files the proof stored itself, which go if it isn't
// kept; the app's own uploads stay.
func (in *PODInput) ownedKeys() []string {
	var keys []string
	for _, f := range in.files() {
		if !f.Uploaded && f.Key != "" {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// files are the proof's stored files as pod_media numbers them: the
// signature at 0, then the photos.
func (in *PODInput) files() []PODFile {
	return append([]PODFile{in.Signature}, in.Photos...)
}

// RecordPOD stores proof of delivery and completes the shipment (FR-020–023).
//
// A proof that arrives after the office changed the shipment — cancelled it,
// gave it to another driver, or completed it with another proof — is not
// lost: it is held for the office to accept or dismiss, and the driver is
// told so with a conflict. A replay of a proof already stored changes
// nothing. RecordPOD owns the stored media: it is kept with the proof, or
// deleted when the proof is refused or was a replay.
func (s *ShipmentService) RecordPOD(ctx context.Context, a Actor, id pgtype.UUID, in PODInput) (shipments.Shipment, error) {
	kept := false
	defer func() {
		if !kept {
			s.media.Delete(context.WithoutCancel(ctx), in.ownedKeys()...)
		}
	}()
	if err := in.Check(); err != nil {
		return shipments.Shipment{}, err
	}
	if in.Signature.Key == "" {
		return shipments.Shipment{}, invalid("signature", "The recipient's signature is required.")
	}
	now := s.now()
	recorded := in.RecordedAt
	if recorded.IsZero() || recorded.After(now) {
		recorded = now
	}

	var out shipments.Shipment
	var result error
	err := s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		cur, err := s.lock(ctx, q, a.TenantID, id)
		if err != nil {
			return err
		}
		out = cur
		driverID := cur.DriverID
		if a.IsDriver() {
			driverID = a.UserID
		}

		if in.ClientID != "" && driverID.Valid {
			prior, err := q.GetPODByClientID(ctx, db.GetPODByClientIDParams{TenantID: a.TenantID, DriverID: driverID, ClientID: &in.ClientID})
			switch {
			case err == nil:
				if prior.Status != PODAccepted {
					result = heldForReview(deref(prior.HeldReason))
				}
				return nil
			case !isNoRows(err):
				return err
			}
		}
		accepted, err := q.GetPODByShipmentID(ctx, db.GetPODByShipmentIDParams{ShipmentID: id, TenantID: a.TenantID})
		hasAccepted := err == nil
		if err != nil && !isNoRows(err) {
			return err
		}
		if hasAccepted && in.ClientID == "" && accepted.DriverID == driverID {
			return nil // a replay from a client that doesn't identify its proofs
		}

		reassigned := a.IsDriver() && cur.DriverID != a.UserID
		if reassigned {
			had, err := s.shipmentData.WasAssignedTo(ctx, q.Conn(), shipments.AssignedQuery{TenantID: a.TenantID, ShipmentID: id, DriverID: a.UserID})
			if err != nil {
				return err
			}
			if !had {
				return notYours(pgtype.UUID{})
			}
		}
		var held string
		switch {
		case cur.Status == model.StatusCancelled:
			held = HeldCancelled
		case reassigned && cur.DriverID.Valid:
			held = HeldReassigned
		case reassigned:
			held = HeldUnassigned
		case hasAccepted:
			held = HeldDuplicate
		}

		verified := false
		var mismatch pgtype.Numeric
		var distance float64
		haveFix := in.Lat != nil && in.Lng != nil && (*in.Lat != 0 || *in.Lng != 0)
		if haveFix && cur.DestinationLat != nil && cur.DestinationLng != nil {
			distance = geo.DistanceMeters(*in.Lat, *in.Lng, *cur.DestinationLat, *cur.DestinationLng)
			verified = distance <= geo.DeliveryTolerance(cur.DestinationPrecise)
			mismatch = toNumeric(float64(int(distance)))
		}
		status := PODAccepted
		if held != "" {
			status = PODHeld
		}
		podID, err := q.CreatePOD(ctx, db.CreatePODParams{
			TenantID:       a.TenantID,
			ShipmentID:     id,
			DriverID:       driverID,
			RecipientName:  truncate(in.RecipientName, 200),
			RecipientPhone: toText(in.RecipientPhone),
			// Written for a release, so a rollback finds them; pod_media
			// is read.
			SignatureUrl:           &in.Signature.Key,
			PhotoUrls:              in.mediaKeys()[1:],
			DeliveryAddress:        &cur.DestinationAddress,
			Latitude:               in.Lat,
			Longitude:              in.Lng,
			DeliveryNotes:          toText(strings.TrimSpace(truncate(in.Notes, 1000))),
			LocationVerified:       &verified,
			LocationMismatchMeters: mismatch,
			LocationReason:         toText(in.LocationReason),
			RecordedAt:             toTimestamp(recorded),
			ClientID:               toText(in.ClientID),
			Status:                 status,
			HeldReason:             toText(held),
		})
		if err != nil {
			return err
		}
		for i, f := range in.files() {
			kind := "photo"
			if i == 0 {
				kind = "signature"
			}
			var taken pgtype.Timestamptz
			if !f.TakenAt.IsZero() {
				taken = toTimestamp(f.TakenAt)
			}
			size, sum := &f.Size, &f.MD5
			if f.Size <= 0 || f.MD5 == "" {
				size, sum = nil, nil
			}
			if err := q.AddPODMedia(ctx, db.AddPODMediaParams{
				TenantID: a.TenantID, PodID: podID, Kind: kind, Position: int16(i), // at most 1 + MaxPhotosPerPOD
				StorageKey: f.Key, ContentType: f.ContentType, SizeBytes: size, Md5: sum,
				TakenAt: taken, Latitude: f.Lat, Longitude: f.Lng,
			}); err != nil {
				return err
			}
		}
		kept = true
		details := map[string]any{
			"recipient": in.RecipientName, "photos": len(in.Photos),
			"location_verified": verified, "distance_m": int(distance), "has_fix": haveFix,
		}

		if held != "" {
			details["pod_id"], details["reason"] = podID.String(), held
			s.event(ctx, q, after, a, cur, shipments.Change{
				Type: "pod_held", Lat: in.Lat, Lng: in.Lng, OccurredAt: recorded, DriverID: driverID,
				Note: "Received by " + in.RecipientName, Details: details,
			})
			result = heldForReview(held)
			return nil
		}

		s.event(ctx, q, after, a, cur, shipments.Change{
			Type: "pod_recorded", Lat: in.Lat, Lng: in.Lng, OccurredAt: recorded,
			Note: "Received by " + in.RecipientName, Details: details,
			POD: &shipments.PODCheck{Driver: driverID, HaveFix: haveFix, Verified: verified, Distance: distance, Reason: in.LocationReason},
		})
		if cur.Status == model.StatusDelivered {
			// The office confirmed it first, by phone or from the driver's
			// SMS; this is the proof it lacked.
			return nil
		}
		out, err = s.applyStatus(ctx, q, after, a, cur, StatusChange{
			Status: model.StatusDelivered, By: model.ActorPOD, OccurredAt: recorded,
			Lat: in.Lat, Lng: in.Lng, recipient: in.RecipientName,
		})
		if errors.Is(err, model.ErrSameStatus) {
			return nil
		}
		return err
	})
	if err != nil {
		kept = false // rolled back: the device sends the images again
		return out, err
	}
	return out, result
}

// ReviewPODs lists a shipment's proofs of delivery that are held for review
// or were dismissed.
func (s *ShipmentService) ReviewPODs(ctx context.Context, tenantID, id pgtype.UUID) ([]db.ListReviewPODsRow, error) {
	return s.q.ListReviewPODs(ctx, db.ListReviewPODsParams{ShipmentID: id, TenantID: tenantID})
}

// AcceptHeldPOD completes a shipment with a proof of delivery that was held
// for review, crediting the delivery to the driver who recorded it. A
// cancelled shipment becomes delivered: the signature says it was.
func (s *ShipmentService) AcceptHeldPOD(ctx context.Context, a Actor, id, podID pgtype.UUID) (shipments.Shipment, error) {
	var out shipments.Shipment
	err := s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		cur, err := s.lock(ctx, q, a.TenantID, id)
		if err != nil {
			return err
		}
		if cur.AnonymisedAt.Valid {
			return errShipmentAnonymised
		}
		pod, err := q.GetHeldPOD(ctx, db.GetHeldPODParams{ID: podID, ShipmentID: id, TenantID: a.TenantID})
		if isNoRows(err) {
			return conflict("This delivery record has already been reviewed.")
		}
		if err != nil {
			return err
		}
		if _, err := q.GetPODByShipmentID(ctx, db.GetPODByShipmentIDParams{ShipmentID: id, TenantID: a.TenantID}); err == nil {
			return conflict("This shipment already has a proof of delivery. Dismiss this one instead.")
		} else if !isNoRows(err) {
			return err
		}

		if cur.DriverID != pod.DriverID {
			previous := cur.DriverID
			if cur, err = s.shipmentData.SetDriver(ctx, q.Conn(), shipments.DriverChange{DriverID: pod.DriverID, ID: id, TenantID: a.TenantID}); err != nil {
				return err
			}
			// The previous driver's app drops the shipment on its next sync.
			s.event(ctx, q, after, a, cur, shipments.Change{
				Type: "assigned", DriverID: pod.DriverID, PreviousDriverID: previous,
				Note: "Credited with the delivery",
			})
		}
		if err := q.ReviewPOD(ctx, db.ReviewPODParams{Status: PODAccepted, ReviewedBy: a.UserID, ID: podID, TenantID: a.TenantID}); err != nil {
			return err
		}
		s.event(ctx, q, after, a, cur, shipments.Change{
			Type: "pod_accepted", DriverID: pod.DriverID,
			Note:    "Received by " + pod.RecipientName,
			Details: map[string]any{"pod_id": podID.String(), "driver": pod.DriverName},
		})
		s.audit(ctx, q, a, "pod_accepted", "shipment", id, map[string]any{"status": cur.Status}, map[string]any{"pod_id": podID.String()})
		out, err = s.applyStatus(ctx, q, after, a, cur, StatusChange{
			Status: model.StatusDelivered, By: model.ActorReview, recipient: pod.RecipientName,
			Note:        fmt.Sprintf("Delivery by %s accepted on review", pod.DriverName),
			deliveredAt: pod.RecordedAt.Time,
		})
		if errors.Is(err, model.ErrSameStatus) {
			out = cur
			return nil
		}
		return err
	})
	return out, err
}

// DismissHeldPOD leaves the shipment as it is. The proof stays on record,
// marked dismissed.
func (s *ShipmentService) DismissHeldPOD(ctx context.Context, a Actor, id, podID pgtype.UUID) error {
	err := s.tx(ctx, func(q *db.Queries, after *afterCommit) error {
		cur, err := s.lock(ctx, q, a.TenantID, id)
		if err != nil {
			return err
		}
		pod, err := q.GetHeldPOD(ctx, db.GetHeldPODParams{ID: podID, ShipmentID: id, TenantID: a.TenantID})
		if isNoRows(err) {
			return conflict("This delivery record has already been reviewed.")
		}
		if err != nil {
			return err
		}
		if err := q.ReviewPOD(ctx, db.ReviewPODParams{Status: PODDismissed, ReviewedBy: a.UserID, ID: podID, TenantID: a.TenantID}); err != nil {
			return err
		}
		s.event(ctx, q, after, a, cur, shipments.Change{
			Type: "pod_dismissed", DriverID: pod.DriverID,
			Note:    "Received by " + pod.RecipientName,
			Details: map[string]any{"pod_id": podID.String(), "driver": pod.DriverName},
		})
		s.audit(ctx, q, a, "pod_dismissed", "shipment", id, nil, map[string]any{"pod_id": podID.String()})
		return nil
	})
	return err
}

// HeldPODMedia finds the stored file n (0 for the signature, then photos)
// of a proof under review, for the shipment page.
func (s *ShipmentService) HeldPODMedia(ctx context.Context, tenantID, id, podID pgtype.UUID, n int) (string, error) {
	pods, err := s.ReviewPODs(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	for _, p := range pods {
		if p.ID == podID {
			return s.PODMedia(ctx, tenantID, podID, n)
		}
	}
	return "", notFound("File not found.")
}

// PODMedia finds the stored file n (0 for the signature, then photos) of a
// proof the caller may see.
func (s *ShipmentService) PODMedia(ctx context.Context, tenantID, podID pgtype.UUID, n int) (string, error) {
	if n < 0 || n > storage.MaxPhotosPerPOD {
		return "", notFound("File not found.")
	}
	f, err := s.q.GetPODMediaKey(ctx, db.GetPODMediaKeyParams{TenantID: tenantID, PodID: podID, Position: int16(n)})
	if isNoRows(err) {
		return "", notFound("File not found.")
	}
	return f.StorageKey, err
}
