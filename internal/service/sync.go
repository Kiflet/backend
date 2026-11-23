package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/db"
	"github.com/kiflet/backend/internal/model"
	"github.com/kiflet/backend/internal/modules/driversync"
	"github.com/kiflet/backend/internal/modules/fleet"
	"github.com/kiflet/backend/internal/modules/proof"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/platform/geo"
	"github.com/kiflet/backend/pkg/metrics"
	"github.com/kiflet/backend/pkg/storage"
)

// SyncService is the driver app's offline sync (FR-009–013): the device
// pushes what it recorded while offline and pulls its current work in one
// round trip.
type SyncService struct {
	*core
	shipments *ShipmentService
	media     *MediaService
}

// SyncRequest is one sync round trip from the driver app.
type SyncRequest struct {
	DeviceID       string     `json:"device_id"`
	AppVersion     string     `json:"app_version"`
	DeviceTime     *time.Time `json:"device_time"`
	Battery        *int       `json:"battery"`
	Pending        *int       `json:"pending"`
	StorageKB      *int       `json:"storage_kb"`
	TrackingPaused bool       `json:"tracking_paused"`
	// PowerSaving says the phone records a position only every 15 minutes,
	// by the driver's choice or because its battery is low.
	PowerSaving bool `json:"power_saving"`
	// Settings holds the settings the driver changed on this phone since
	// the last sync.
	Settings *SettingsChange `json:"settings,omitempty"`
	// Since is the cursor from the previous response; empty on first sync.
	// A timestamp, from a PWA loaded before the driver feed, gets a full
	// download.
	Since string `json:"since"`
	// ReceivedAt is when the request reached the server, set by the handler
	// before it reads the body: the moment to compare DeviceTime with.
	ReceivedAt time.Time `json:"-"`

	Statuses  []SyncStatus   `json:"statuses"`
	PODs      []SyncPOD      `json:"pods"`
	Locations []SyncLocation `json:"locations"`
	Messages  []SyncMessage  `json:"messages"`
}

// SyncStatus is a status change made on the device.
type SyncStatus struct {
	ClientID   string    `json:"client_id"`
	ShipmentID string    `json:"shipment_id"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	Note       string    `json:"note"`
	Lat        *float64  `json:"lat"`
	Lng        *float64  `json:"lng"`
	RecordedAt time.Time `json:"recorded_at"`
}

// SyncPOD is a proof of delivery captured on the device. Images are data
// URLs.
type SyncPOD struct {
	ClientID       string   `json:"client_id"`
	ShipmentID     string   `json:"shipment_id"`
	RecipientName  string   `json:"recipient_name"`
	RecipientPhone string   `json:"recipient_phone"`
	Signature      string   `json:"signature"`
	Photos         []string `json:"photos"`
	Notes          string   `json:"notes"`
	Lat            *float64 `json:"lat"`
	Lng            *float64 `json:"lng"`
	// LocationReason is why the driver delivered away from the destination
	// after the app warned them.
	LocationReason string    `json:"location_reason"`
	RecordedAt     time.Time `json:"recorded_at"`
	// SignatureFile and PhotoFiles are the files the app uploaded first
	// (POST /api/v1/driver/uploads), named by key, the app's way;
	// Signature and Photos are data URLs, the PWA's. A delivery sends one
	// or the other.
	SignatureFile *SyncPODFile  `json:"signature_file,omitempty"`
	PhotoFiles    []SyncPODFile `json:"photo_files,omitempty"`
}

// SyncPODFile is an uploaded file of a delivery, with when and where it
// was taken (AC-021.3, AC-021.4).
type SyncPODFile struct {
	Key     string     `json:"key"`
	TakenAt *time.Time `json:"taken_at,omitempty"`
	Lat     *float64   `json:"lat,omitempty"`
	Lng     *float64   `json:"lng,omitempty"`
}

// SyncLocation is a GPS fix.
type SyncLocation struct {
	ClientID   string    `json:"client_id"`
	ShipmentID string    `json:"shipment_id"`
	Lat        float64   `json:"lat"`
	Lng        float64   `json:"lng"`
	Accuracy   *float64  `json:"accuracy"`
	Speed      *float64  `json:"speed"`
	Heading    *float64  `json:"heading"`
	Battery    *int      `json:"battery"`
	RecordedAt time.Time `json:"recorded_at"`
}

// SyncMessage is a note from the driver to the office.
type SyncMessage struct {
	ClientID   string    `json:"client_id"`
	ShipmentID string    `json:"shipment_id"`
	Text       string    `json:"text"`
	RecordedAt time.Time `json:"recorded_at"`
}

// SyncResult tells the device what became of one queued item. OK and
// rejected items leave the queue; Retry ones stay for the next sync.
type SyncResult struct {
	ClientID string `json:"client_id"`
	Kind     string `json:"kind"`
	OK       bool   `json:"ok"`
	Retry    bool   `json:"retry,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

// SyncResponse is the server's half of the round trip (docs/architecture.md,
// section 11, "The driver sync").
type SyncResponse struct {
	ServerTime time.Time `json:"server_time"`
	// Cursor is passed back as Since next time: an opaque string.
	Cursor string `json:"cursor"`
	// Full says this is a full download: Shipments are all the driver
	// should hold, and the phone lets go of the others, keeping any with
	// changes not yet sent or a delivery draft.
	Full            bool         `json:"full"`
	Results         []SyncResult `json:"results"`
	LocationsStored int          `json:"locations_stored"`
	// Shipments are the loads that changed since the cursor, as they are
	// now.
	Shipments []DriverShipment `json:"shipments"`
	// Removed are the loads taken from the driver since the cursor, and
	// why; RemovedShipmentIDs are their IDs alone, for the PWA.
	Removed            []RemovedShipment `json:"removed"`
	RemovedShipmentIDs []string          `json:"removed_shipment_ids"`
	Messages           []DriverMessage   `json:"messages"`
	// Driver is the profile, when it changed since the cursor.
	Driver *DriverProfile `json:"driver,omitempty"`
	// More says there is more than one response carries: the app syncs
	// again at once.
	More bool `json:"more,omitempty"`
	// ConfigETag is the driver config's ETag: when it isn't the one the
	// app holds, the app fetches the config again.
	ConfigETag string `json:"config_etag"`
}

// RemovedShipment is a load taken off the driver's phone.
type RemovedShipment struct {
	ID string `json:"id"`
	// Reason is reassigned, unassigned or cancelled.
	Reason string `json:"reason"`
}

// DriverShipment is a shipment as the driver app holds it.
type DriverShipment struct {
	ID                 string   `json:"id"`
	TrackingNumber     string   `json:"tracking_number"`
	Ref                string   `json:"ref"`
	Status             string   `json:"status"`
	StatusReason       string   `json:"status_reason,omitempty"`
	Priority           string   `json:"priority"`
	OriginCity         string   `json:"origin_city"`
	OriginAddress      string   `json:"origin_address"`
	OriginLat          *float64 `json:"origin_lat"`
	OriginLng          *float64 `json:"origin_lng"`
	DestinationCity    string   `json:"destination_city"`
	DestinationAddress string   `json:"destination_address"`
	DestinationLat     *float64 `json:"destination_lat"`
	DestinationLng     *float64 `json:"destination_lng"`
	DestinationPrecise bool     `json:"destination_precise"`
	// ToleranceM is how far from the destination a delivery may be recorded
	// before the app asks why.
	ToleranceM    float64 `json:"tolerance_m"`
	CustomerName  string  `json:"customer_name"`
	CustomerPhone string  `json:"customer_phone"`
	// ConsigneeName and ConsigneePhone are who takes delivery, and empty
	// when that is the customer.
	ConsigneeName  string  `json:"consignee_name"`
	ConsigneePhone string  `json:"consignee_phone"`
	Cargo          string  `json:"cargo"`
	WeightKg       float64 `json:"weight_kg"`
	// Pieces is the piece count as written, e.g. "152 bales", or empty.
	Pieces        string     `json:"pieces"`
	Instructions  string     `json:"instructions"`
	Vehicle       string     `json:"vehicle"`
	ETA           *time.Time `json:"eta"`
	PlannedPickup *time.Time `json:"planned_pickup"`
	PickedUpAt    *time.Time `json:"picked_up_at"`
	DeliveredAt   *time.Time `json:"delivered_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	// RecentRecipients are people who signed for this receiver before,
	// newest first, to suggest on the delivery screen.
	RecentRecipients []string `json:"recent_recipients"`
}

// DriverMessage is a note from the office, shown in the app.
type DriverMessage struct {
	ID             string `json:"id"`
	ShipmentID     string `json:"shipment_id"`
	TrackingNumber string `json:"tracking_number"`
	// Text is the message as shown, in English for what the office did.
	Text string `json:"text"`
	// Kind is "cancelled" or "reopened" when the office did that to the
	// shipment, for the app to say in the driver's language; Note is then
	// what the office wrote, if anything.
	Kind string    `json:"kind,omitempty"`
	Note string    `json:"note,omitempty"`
	From string    `json:"from"`
	At   time.Time `json:"at"`
}

// DriverProfile is who the app is signed in as.
type DriverProfile struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Phone         string `json:"phone"`
	Company       string `json:"company"`
	Vehicle       string `json:"vehicle"`
	MustChangePIN bool   `json:"must_change_pin"`
	SMSNumber     string `json:"sms_number"`
	// OfficePhone is the company's number, for the driver to call.
	OfficePhone string         `json:"office_phone"`
	Settings    DriverSettings `json:"settings"`
}

// DriverSettings are the driver app's settings, kept on the server so they
// follow the driver to a new phone (FR-026 AC-026.6).
type DriverSettings struct {
	// Language is "en" or "am", or empty until the driver chooses one.
	Language     string `json:"language"`
	BatterySaver bool   `json:"battery_saver"`
	Vibrate      bool   `json:"vibrate"`
}

// SettingsChange is what the driver changed on the phone. Settings left out
// stay as they are, so a language picked at sign-in on a new phone doesn't
// undo the others.
type SettingsChange struct {
	Language     *string `json:"language"`
	BatterySaver *bool   `json:"battery_saver"`
	Vibrate      *bool   `json:"vibrate"`
}

// recentRecipients is how many earlier recipients the app suggests.
const recentRecipients = 5

// Sync limits per request, beyond which the device sends the rest next time.
const (
	maxSyncStatuses  = 200
	maxSyncPODs      = 20
	maxSyncLocations = 5000
	maxSyncMessages  = 100
)

// Sync processes a round trip.
func (s *SyncService) Sync(ctx context.Context, p *Principal, req SyncRequest, ci ClientInfo) (*SyncResponse, error) {
	if !p.IsDriver() {
		return nil, ErrNotDriver
	}
	now := s.now()
	actor := p.Actor(SourceApp, ci.IP, ci.UserAgent)

	// Phones are often set to the wrong time, and everything the device
	// recorded is stamped with its clock. The difference between its clock
	// and ours when the request arrived corrects every timestamp in it,
	// however small: a phone a minute slow would otherwise report a move
	// made just after the office's as made before it, and have it refused
	// as out of date. Arrived means when the request's headers did, before
	// a body with photos had uploaded, so a slow upload doesn't pass for a
	// slow clock.
	arrived := req.ReceivedAt
	if arrived.IsZero() || arrived.After(now) || now.Sub(arrived) > 10*time.Minute {
		arrived = now
	}
	var offset time.Duration
	if req.DeviceTime != nil {
		offset = arrived.Sub(*req.DeviceTime)
	}
	correct := func(t time.Time) time.Time {
		if t.IsZero() {
			return now
		}
		t = t.Add(offset)
		if t.After(now) {
			return now
		}
		return t
	}

	resp := &SyncResponse{ServerTime: now, Results: []SyncResult{}, Shipments: []DriverShipment{},
		Removed: []RemovedShipment{}, RemovedShipmentIDs: []string{}, Messages: []DriverMessage{}}

	// Status changes and deliveries are applied in the order they happened,
	// so "arrived" lands before the POD that completes the shipment.
	type op struct {
		at     time.Time
		status *SyncStatus
		pod    *SyncPOD
	}
	var ops []op
	for i := range capSlice(req.Statuses, maxSyncStatuses) {
		ops = append(ops, op{at: correct(req.Statuses[i].RecordedAt), status: &req.Statuses[i]})
	}
	for i := range capSlice(req.PODs, maxSyncPODs) {
		ops = append(ops, op{at: correct(req.PODs[i].RecordedAt), pod: &req.PODs[i]})
	}
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].at.Before(ops[j].at) })

	for _, o := range ops {
		if o.status != nil {
			resp.Results = append(resp.Results, s.applyStatus(ctx, actor, *o.status, o.at))
		} else {
			resp.Results = append(resp.Results, s.applyPOD(ctx, actor, *o.pod, o.at))
		}
	}

	for _, m := range capSlice(req.Messages, maxSyncMessages) {
		r := SyncResult{ClientID: m.ClientID, Kind: "message", OK: true}
		id, err := parseUUID(m.ShipmentID)
		if err == nil {
			_, err = s.shipments.Note(ctx, actor, id, m.Text, false, false, m.ClientID, correct(m.RecordedAt))
		} else {
			err = notFound("Shipment not found.")
		}
		resp.Results = append(resp.Results, resultFor(r, err))
	}

	// Positions may only be attributed to the driver's own shipments.
	mine, err := s.shipmentData.ForDriverSync(ctx, s.q.Conn(), shipments.SyncQuery{DriverID: p.UserID, TenantID: p.TenantID})
	if err != nil {
		return nil, err
	}
	allowed := make(map[pgtype.UUID]bool, len(mine))
	for _, sh := range mine {
		allowed[sh.ID] = true
	}
	if locs := capSlice(req.Locations, maxSyncLocations); len(locs) > 0 {
		// Points recorded offline for a shipment handed to another driver
		// meanwhile are still this driver's trip: kept, if it was theirs.
		checked := map[pgtype.UUID]bool{}
		for _, l := range locs {
			id := toUUID(l.ShipmentID)
			if !id.Valid || allowed[id] || checked[id] {
				continue
			}
			checked[id] = true
			had, err := s.shipmentData.WasAssignedTo(ctx, s.q.Conn(), shipments.AssignedQuery{TenantID: p.TenantID, ShipmentID: id, DriverID: p.UserID})
			if err != nil {
				return nil, err
			}
			allowed[id] = had
		}
		pings := make([]Ping, 0, len(locs))
		for _, l := range locs {
			pings = append(pings, Ping{
				ClientID: l.ClientID, ShipmentID: toUUID(l.ShipmentID), Lat: l.Lat, Lng: l.Lng,
				Accuracy: l.Accuracy, Speed: l.Speed, Heading: l.Heading, Battery: l.Battery,
				RecordedAt: correct(l.RecordedAt),
			})
		}
		tracking := &TrackingService{core: s.core}
		resp.LocationsStored, _ = tracking.record(ctx, actor, req.DeviceID, pings, allowed)
	}

	for _, r := range resp.Results {
		metrics.SyncChanges.WithLabelValues(r.Kind, syncOutcome(r)).Inc()
	}
	metrics.SyncLocations.Add(float64(resp.LocationsStored))

	var battery, pending, storageKB, clockOffset *int32
	if req.Battery != nil && *req.Battery >= 0 && *req.Battery <= 100 {
		battery = ptr(int32(*req.Battery))
	}
	if req.Pending != nil {
		pending = ptr(int32(*req.Pending))
	}
	if req.StorageKB != nil {
		storageKB = ptr(int32(*req.StorageKB))
	}
	if req.DeviceTime != nil {
		clockOffset = ptr(int32(offset.Seconds()))
	}
	if err := s.q.UpsertSyncMetadata(ctx, db.UpsertSyncMetadataParams{
		DriverID: p.UserID, DeviceID: toText(truncate(req.DeviceID, 100)), AppVersion: toText(truncate(req.AppVersion, 40)),
		BatteryLevel: battery, PendingSyncCount: pending, StorageUsedKb: storageKB,
		ClockOffsetSeconds: clockOffset, TrackingPaused: req.TrackingPaused, PowerSaving: req.PowerSaving,
	}); err != nil {
		s.log.Error().Err(err).Msg("upsert sync metadata")
	}
	if st := req.Settings; st != nil {
		lang := st.Language
		if lang != nil && *lang != "en" && *lang != "am" {
			lang = nil
		}
		if err := s.fleet.SetDriverAppSettings(ctx, s.q.Conn(), p.TenantID, p.UserID, fleet.AppSettings{
			Language: lang, BatterySaver: st.BatterySaver, Vibrate: st.Vibrate,
		}); err != nil {
			return nil, err
		}
	}
	_ = s.q.AcknowledgeDriverAlerts(ctx, db.AcknowledgeDriverAlertsParams{DriverID: p.UserID, AlertType: AlertDriverOffline})

	if err := s.pull(ctx, p, req.Since, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func capSlice[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *SyncService) applyStatus(ctx context.Context, a Actor, st SyncStatus, at time.Time) SyncResult {
	r := SyncResult{ClientID: st.ClientID, Kind: "status", OK: true}
	id, err := parseUUID(st.ShipmentID)
	if err != nil {
		return resultFor(r, notFound("Shipment not found."))
	}
	_, err = s.shipments.ChangeStatus(ctx, a, id, StatusChange{
		Status: st.Status, Reason: st.Reason, Note: st.Note, Lat: st.Lat, Lng: st.Lng,
		OccurredAt: at, By: model.ActorDriver, ClientID: st.ClientID,
	})
	if errors.Is(err, model.ErrSameStatus) {
		err = nil
	}
	return resultFor(r, err)
}

func (s *SyncService) applyPOD(ctx context.Context, a Actor, in SyncPOD, at time.Time) SyncResult {
	r := SyncResult{ClientID: in.ClientID, Kind: "pod", OK: true}
	id, err := parseUUID(in.ShipmentID)
	if err != nil {
		return resultFor(r, notFound("Shipment not found."))
	}
	pod := PODInput{
		RecipientName: in.RecipientName, RecipientPhone: in.RecipientPhone, Notes: in.Notes,
		Lat: in.Lat, Lng: in.Lng, LocationReason: in.LocationReason, RecordedAt: at, ClientID: in.ClientID,
	}
	// Whatever will be refused is refused before any image is stored.
	if err := pod.Check(); err != nil {
		return resultFor(r, err)
	}
	// A replay of a proof already stored must not upload its images again.
	if in.ClientID != "" {
		prior, err := s.q.GetPODByClientID(ctx, db.GetPODByClientIDParams{TenantID: a.TenantID, DriverID: a.UserID, ClientID: &in.ClientID})
		if err == nil {
			if prior.Status != PODAccepted {
				return resultFor(r, heldForReview(deref(prior.HeldReason)))
			}
			return r
		}
	}
	sh, err := s.shipments.Get(ctx, a.TenantID, id)
	if err != nil {
		return resultFor(r, err)
	}
	if sh.DriverID != a.UserID {
		// Handed over while this driver was offline: their proof is held for
		// review, but only if the shipment was ever theirs.
		had, err := s.shipmentData.WasAssignedTo(ctx, s.q.Conn(), shipments.AssignedQuery{TenantID: a.TenantID, ShipmentID: id, DriverID: a.UserID})
		if err != nil {
			return resultFor(r, err)
		}
		if !had {
			return resultFor(r, notYours(pgtype.UUID{}))
		}
	}
	if in.SignatureFile != nil || len(in.PhotoFiles) > 0 {
		pod.Signature, pod.Photos, err = s.uploadedFiles(ctx, a, in)
	} else {
		pod.Signature, pod.Photos, err = s.media.SavePOD(ctx, id.String(), in.Signature, in.Photos)
	}
	if err != nil {
		return resultFor(r, err)
	}
	_, err = s.shipments.RecordPOD(ctx, a, id, pod)
	return resultFor(r, err)
}

// errFilesMissing is a delivery naming files not yet in storage.
var errFilesMissing = &Error{Kind: KindConflict, Code: driversync.RefusedFilesMissing, Message: "The photos haven't reached the server yet. They will be sent again."}

// maxPODPhotos is the most photos a delivery has, as the config tells the
// app (max_photos).
const maxPODPhotos = 3

// uploadedFiles are a delivery's files the app uploaded, found by their
// keys: the driver's own, of the kind named, and in storage.
func (s *SyncService) uploadedFiles(ctx context.Context, a Actor, in SyncPOD) (PODFile, []PODFile, error) {
	if in.Signature != "" || len(in.Photos) > 0 {
		return PODFile{}, nil, invalid("signature_file", "Send a delivery's files by key or inline, not both.")
	}
	if in.SignatureFile == nil {
		return PODFile{}, nil, invalid("signature_file", "The recipient's signature is required.")
	}
	if len(in.PhotoFiles) > maxPODPhotos {
		return PODFile{}, nil, invalid("photo_files", fmt.Sprintf("A delivery has at most %d photos.", maxPODPhotos))
	}
	if s.uploads == nil {
		return PODFile{}, nil, errors.New("uploads are not configured")
	}
	d := proof.Driver{TenantID: a.TenantID, UserID: a.UserID}
	file := func(kind string, f SyncPODFile) (PODFile, error) {
		u, err := s.uploads.Find(ctx, d, kind, f.Key)
		if errors.Is(err, proof.ErrNotStored) {
			return PODFile{}, errFilesMissing
		}
		if err != nil {
			return PODFile{}, err
		}
		out := PODFile{File: storage.File{Key: u.Key, ContentType: u.ContentType, Size: u.Size, MD5: u.MD5}, Uploaded: true}
		if f.TakenAt != nil && !f.TakenAt.After(s.now()) {
			out.TakenAt = *f.TakenAt
		}
		if f.Lat != nil && f.Lng != nil && geo.Valid(*f.Lat, *f.Lng) {
			out.Lat, out.Lng = f.Lat, f.Lng
		}
		return out, nil
	}
	sig, err := file(proof.KindSignature, *in.SignatureFile)
	if err != nil {
		return PODFile{}, nil, err
	}
	photos := make([]PODFile, 0, len(in.PhotoFiles))
	for _, f := range in.PhotoFiles {
		p, err := file(proof.KindPhoto, f)
		if err != nil {
			return PODFile{}, nil, err
		}
		photos = append(photos, p)
	}
	return sig, photos, nil
}

// syncOutcome names a result for /metrics.
func syncOutcome(r SyncResult) string {
	switch {
	case r.OK:
		return "applied"
	case r.Retry:
		return "retry"
	}
	return "refused"
}

// resultFor classifies an outcome by the sync's refusal codes
// (driversync.Refused*; docs/architecture.md, section 11, "Refused
// items"): an error that names one keeps it; otherwise a missing or
// forbidden load is not_found, a move the load's state no longer allows
// (the office changed it meanwhile) stale, and other bad input invalid.
// These are final; anything else is a fault on our side, retried.
func resultFor(r SyncResult, err error) SyncResult {
	if err == nil {
		return r
	}
	r.OK = false
	de, ok := AsError(err)
	if !ok {
		r.Retry = true
		r.Code, r.Message = driversync.RefusedServerError, "The server couldn't save this. It will be retried."
		return r
	}
	r.Message = de.Message
	switch {
	case slices.Contains(driversync.Refusals, de.Code):
		r.Code = de.Code
		// Not yet, rather than not at all.
		r.Retry = de.Code == driversync.RefusedFilesMissing
	case errors.Is(err, ErrStaleUpdate), de.Kind == KindConflict:
		r.Code = driversync.RefusedStale
	case de.Kind == KindNotFound, de.Kind == KindForbidden:
		r.Code = driversync.RefusedNotFound
	default:
		r.Code = driversync.RefusedInvalid
	}
	return r
}

// feedPage is the most feed entries one sync reads; with more, the app
// syncs again at once.
const feedPage = 500

// fullMessages is how many of the newest messages, and of the newest
// broadcasts, from the last week a full download carries: the PWA keeps
// 50.
const fullMessages = 50

// pull fills in what the device should now hold, read in one snapshot:
// what changed since the cursor, from the driver's feed, or everything
// when the cursor can't be carried on from (docs/architecture.md, section
// 11, "The driver sync").
func (s *SyncService) pull(ctx context.Context, p *Principal, since string, resp *SyncResponse) error {
	return s.snapshot(ctx, func(q *db.Queries) error {
		head, err := s.feed.Head(ctx, q.Conn(), p.TenantID, p.UserID)
		if err != nil {
			return err
		}
		profile, err := s.profile(ctx, q, p)
		if err != nil {
			return err
		}
		digest := profileDigest(profile)
		if c, ok := parseSyncCursor(since); ok && c.driver == p.UserID.Bytes && head.Honours(c.seq) {
			return s.pullChanges(ctx, q, p, c, profile, digest, resp)
		}
		var legacy *time.Time
		if t, err := time.Parse(time.RFC3339Nano, since); err == nil {
			legacy = &t
		}
		return s.pullAll(ctx, q, p, head, legacy, profile, digest, resp)
	})
}

// pullChanges sends what the feed says changed after the cursor: each load
// once, as it is now, those no longer the driver's as removed, and the new
// messages.
func (s *SyncService) pullChanges(ctx context.Context, q *db.Queries, p *Principal, c syncCursor, profile DriverProfile, digest uint64, resp *SyncResponse) error {
	items, err := s.feed.Since(ctx, q.Conn(), p.TenantID, p.UserID, c.seq, feedPage)
	if err != nil {
		return err
	}
	resp.More = len(items) == feedPage
	last := c.seq
	type record struct {
		kind string
		ref  pgtype.UUID
	}
	latest := map[record]driversync.Item{}
	var order []record
	for _, it := range items {
		r := record{it.Kind, it.RefID}
		if _, seen := latest[r]; !seen {
			order = append(order, r)
		}
		latest[r] = it
		last = it.Seq
	}
	var loads, notes, broadcasts []pgtype.UUID
	for _, r := range order {
		switch r.kind {
		case driversync.KindShipment:
			loads = append(loads, r.ref)
		case driversync.KindMessage:
			notes = append(notes, r.ref)
		case driversync.KindBroadcast:
			broadcasts = append(broadcasts, r.ref)
		}
	}

	if len(loads) > 0 {
		ships, err := s.shipmentData.ByIDs(ctx, q.Conn(), p.TenantID, loads)
		if err != nil {
			return err
		}
		byID := make(map[pgtype.UUID]shipments.Shipment, len(ships))
		for _, sh := range ships {
			byID[sh.ID] = sh
		}
		var mine []shipments.Shipment
		for _, id := range loads {
			sh, ok := byID[id]
			switch {
			case ok && sh.DriverID == p.UserID:
				mine = append(mine, sh)
			case !ok:
				// Deleted since: gone from every phone.
				s.removed(resp, id, driversync.Unassigned)
			default:
				why := latest[record{driversync.KindShipment, id}].Reason
				if why == "" {
					// Taken away after this page's entries.
					why = driversync.RemovalReason(sh.Status, sh.DriverID.Valid)
				}
				s.removed(resp, id, why)
			}
		}
		if err := s.addShipments(ctx, q, p, mine, resp); err != nil {
			return err
		}
	}
	if err := s.addMessages(ctx, q, p, notes, broadcasts, time.Unix(0, 0), max(len(notes), len(broadcasts)), resp); err != nil {
		return err
	}
	if digest != c.profile {
		resp.Driver = &profile
	}
	resp.Cursor = syncCursor{driver: p.UserID.Bytes, seq: last, profile: digest}.String()
	return nil
}

// pullAll is a full download: everything the driver should hold, as of the
// feed's head. A timestamp cursor, from a PWA loaded before the feed, also
// gets the loads taken from the driver since then, which that PWA reads.
func (s *SyncService) pullAll(ctx context.Context, q *db.Queries, p *Principal, head driversync.Head, legacy *time.Time, profile DriverProfile, digest uint64, resp *SyncResponse) error {
	resp.Full = true
	ships, err := s.shipmentData.ForDriverSync(ctx, q.Conn(), shipments.SyncQuery{DriverID: p.UserID, TenantID: p.TenantID})
	if err != nil {
		return err
	}
	if err := s.addShipments(ctx, q, p, ships, resp); err != nil {
		return err
	}
	if legacy != nil {
		removed, err := s.shipmentData.UnassignedFromDriverSince(ctx, q.Conn(), shipments.UnassignedQuery{
			TenantID: p.TenantID, DriverID: p.UserID, Since: toTimestamp(*legacy),
		})
		if err != nil {
			return err
		}
		for _, id := range removed {
			resp.RemovedShipmentIDs = append(resp.RemovedShipmentIDs, id.String())
		}
	}
	if err := s.addMessages(ctx, q, p, nil, nil, resp.ServerTime.Add(-7*24*time.Hour), fullMessages, resp); err != nil {
		return err
	}
	resp.Driver = &profile
	resp.Cursor = syncCursor{driver: p.UserID.Bytes, seq: head.Seq, profile: digest}.String()
	return nil
}

func (s *SyncService) removed(resp *SyncResponse, id pgtype.UUID, reason string) {
	resp.Removed = append(resp.Removed, RemovedShipment{ID: id.String(), Reason: reason})
	resp.RemovedShipmentIDs = append(resp.RemovedShipmentIDs, id.String())
}

// addShipments adds loads as the app holds them, with the vehicle's plate
// and the people who signed for each receiver before.
func (s *SyncService) addShipments(ctx context.Context, q *db.Queries, p *Principal, ships []shipments.Shipment, resp *SyncResponse) error {
	if len(ships) == 0 {
		return nil
	}
	plates := map[pgtype.UUID]string{}
	if vs, err := s.activeVehicles(ctx, q, p.TenantID); err == nil {
		for _, v := range vs {
			plates[v.ID] = v.PlateNumber
		}
	}
	phones := make([]string, 0, len(ships))
	for _, sh := range ships {
		_, to := receiver(sh)
		phones = append(phones, to)
	}
	signed := map[string][]string{}
	rows, err := q.ListRecentRecipients(ctx, db.ListRecentRecipientsParams{TenantID: p.TenantID, Phones: phones})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if len(signed[r.ReceiverPhone]) < recentRecipients {
			signed[r.ReceiverPhone] = append(signed[r.ReceiverPhone], r.RecipientName)
		}
	}
	for _, sh := range ships {
		ds := toDriverShipment(sh, plates[sh.VehicleID])
		_, to := receiver(sh)
		ds.RecentRecipients = signed[to]
		if ds.RecentRecipients == nil {
			ds.RecentRecipients = []string{}
		}
		resp.Shipments = append(resp.Shipments, ds)
	}
	return nil
}

// addMessages adds the office's notes and broadcasts: those of the IDs
// given, or with none given (nil) the newest since a time, up to limit of
// each.
func (s *SyncService) addMessages(ctx context.Context, q *db.Queries, p *Principal, notes, broadcasts []pgtype.UUID, since time.Time, limit int, resp *SyncResponse) error {
	full := notes == nil && broadcasts == nil
	if full || len(notes) > 0 {
		msgs, err := s.shipmentData.DriverMessages(ctx, q.Conn(), shipments.DriverMessagesQuery{
			TenantID: p.TenantID, DriverID: p.UserID, Ids: notes, Since: toTimestamp(since), MaxMessages: int32(limit),
		})
		if err != nil {
			return err
		}
		for _, m := range msgs {
			resp.Messages = append(resp.Messages, toDriverMessage(m))
		}
	}
	if full || len(broadcasts) > 0 {
		// Messages to every driver on the road belong to no shipment.
		all, err := q.ListDriverBroadcasts(ctx, db.ListDriverBroadcastsParams{
			TenantID: p.TenantID, DriverID: p.UserID, Ids: broadcasts, Since: toTimestamp(since), MaxMessages: int32(limit),
		})
		if err != nil {
			return err
		}
		for _, m := range all {
			resp.Messages = append(resp.Messages, DriverMessage{ID: m.ID.String(), Text: m.Body, From: m.SenderName, At: m.CreatedAt.Time})
		}
	}
	return nil
}

func toDriverMessage(m shipments.DriverMessage) DriverMessage {
	dm := DriverMessage{
		ID: m.ID.String(), ShipmentID: m.ShipmentID.String(), TrackingNumber: m.TrackingNumber,
		Text: deref(m.Note), From: m.ActorName, At: m.CreatedAt.Time,
	}
	if m.EventType == "status_changed" {
		// The office cancelled or reopened the shipment: said even when it
		// wrote nothing.
		if deref(m.ToStatus) == model.StatusCancelled {
			dm.Kind, dm.Note = "cancelled", dm.Text
			dm.Text = "The office cancelled this shipment."
		} else {
			dm.Kind = "reopened"
			dm.Text = "The office reopened this shipment."
		}
		if dm.Note != "" {
			dm.Text += " " + dm.Note
		}
	}
	return dm
}

// profile is who the app is signed in as.
func (s *SyncService) profile(ctx context.Context, q *db.Queries, p *Principal) (DriverProfile, error) {
	u, err := s.user(ctx, q, p.TenantID, p.UserID)
	if err != nil {
		return DriverProfile{}, err
	}
	d := DriverProfile{
		ID: u.ID.String(), Name: u.FullName, Phone: deref(u.Phone), Company: p.TenantName,
		MustChangePIN: u.PasswordResetRequired, SMSNumber: s.cfg.SMSNumber,
		Settings: DriverSettings{Language: deref(u.Language), BatterySaver: u.AppBatterySaver, Vibrate: u.AppVibrate},
	}
	if t, err := s.tenant(ctx, q, p.TenantID); err == nil {
		d.OfficePhone = deref(t.ContactPhone)
	}
	if u.VehicleID.Valid {
		if v, err := s.vehicle(ctx, q, p.TenantID, u.VehicleID); err == nil {
			d.Vehicle = v.PlateNumber
		}
	}
	return d, nil
}

func toDriverShipment(sh shipments.Shipment, plate string) DriverShipment {
	return DriverShipment{
		ID:                 sh.ID.String(),
		TrackingNumber:     sh.TrackingNumber,
		Ref:                ShortRef(sh.TrackingNumber),
		Status:             sh.Status,
		StatusReason:       deref(sh.StatusReason),
		Priority:           sh.Priority,
		OriginCity:         deref(sh.OriginCity),
		OriginAddress:      sh.OriginAddress,
		OriginLat:          sh.OriginLat,
		OriginLng:          sh.OriginLng,
		DestinationCity:    deref(sh.DestinationCity),
		DestinationAddress: sh.DestinationAddress,
		DestinationLat:     sh.DestinationLat,
		DestinationLng:     sh.DestinationLng,
		DestinationPrecise: sh.DestinationPrecise,
		ToleranceM:         geo.DeliveryTolerance(sh.DestinationPrecise),
		CustomerName:       sh.CustomerName,
		CustomerPhone:      sh.CustomerPhone,
		ConsigneeName:      deref(sh.ConsigneeName),
		ConsigneePhone:     deref(sh.ConsigneePhone),
		Cargo:              deref(sh.CargoDescription),
		WeightKg:           numericOr(sh.CargoWeight),
		Pieces:             FormatPieces(sh.CargoPieces, sh.CargoUnit),
		Instructions:       deref(sh.SpecialInstructions),
		Vehicle:            plate,
		ETA:                timePtr(sh.EstimatedDelivery),
		PlannedPickup:      timePtr(sh.PlannedPickupAt),
		PickedUpAt:         timePtr(sh.PickedUpAt),
		DeliveredAt:        timePtr(sh.ActualDelivery),
		CreatedAt:          sh.CreatedAt.Time,
		UpdatedAt:          sh.UpdatedAt.Time,
	}
}
