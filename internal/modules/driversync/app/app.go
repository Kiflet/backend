// Package app is the driversync module's implementation of driversync.Feed.
package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kiflet/backend/internal/modules/driversync"
	"github.com/kiflet/backend/internal/modules/driversync/store"
	"github.com/kiflet/backend/internal/modules/shipments"
	"github.com/kiflet/backend/internal/platform/database"
	"github.com/kiflet/backend/internal/platform/events"
)

// Service is driversync.Feed on Postgres.
type Service struct {
	q *store.Queries
}

var _ driversync.Feed = (*Service)(nil)

// New is the driversync module, reading through conn (the pool). With a
// bus, it writes the entries for shipment events itself.
func New(conn database.Conn, bus events.Bus) *Service {
	s := &Service{q: store.New(conn)}
	if bus != nil {
		bus.Within("shipment.*", s.shipmentChanged)
	}
	return s
}

// shipmentChanged writes what a change to a load means for drivers'
// phones: the load for its driver, a removal for the driver it was taken
// from, and the office's note for the driver it was addressed to.
func (s *Service) shipmentChanged(ctx context.Context, tx events.Tx, e events.Event) error {
	m, ok := e.Payload.(shipments.Changed)
	if !ok {
		return nil
	}
	return s.Add(ctx, tx, ForChange(m)...)
}

// ForChange are the entries a shipment event writes.
func ForChange(m shipments.Changed) []driversync.Entry {
	sh, c := m.Shipment, m.Change
	var out []driversync.Entry
	// A note leaves the load as it was.
	if sh.DriverID.Valid && c.Type != "note" && c.Type != "message" {
		out = append(out, driversync.Entry{
			TenantID: sh.TenantID, DriverID: sh.DriverID, Kind: driversync.KindShipment, RefID: sh.ID, Op: driversync.OpUpsert,
		})
	}
	if c.PreviousDriverID.Valid && c.PreviousDriverID != sh.DriverID {
		out = append(out, driversync.Entry{
			TenantID: sh.TenantID, DriverID: c.PreviousDriverID, Kind: driversync.KindShipment, RefID: sh.ID,
			Op: driversync.OpRemove, Reason: driversync.RemovalReason(sh.Status, sh.DriverID.Valid),
		})
	}
	// What the office addressed to the driver; never the driver's own.
	if c.VisibleToDriver && c.DriverID.Valid && m.EventID.Valid && m.Actor.UserID != c.DriverID {
		out = append(out, driversync.Entry{
			TenantID: sh.TenantID, DriverID: c.DriverID, Kind: driversync.KindMessage, RefID: m.EventID, Op: driversync.OpUpsert,
		})
	}
	return out
}

// pending are the entries a transaction has added so far.
type pending struct{ entries []driversync.Entry }

type pendingKey struct{}

func (s *Service) Add(ctx context.Context, tx events.Tx, entries ...driversync.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	if tx.Scope == nil {
		return write(ctx, tx.Conn, entries)
	}
	p := tx.Scope.Value(pendingKey{}, func() any {
		p := &pending{}
		tx.Scope.BeforeCommit(func(ctx context.Context) error { return write(ctx, tx.Conn, p.entries) })
		return p
	}).(*pending)
	p.entries = append(p.entries, entries...)
	return nil
}

// write numbers and writes entries, a driver at a time in order of driver
// ID: every transaction takes the heads' locks in the same order. Of
// several entries for one record of one driver, the last says it all.
func write(ctx context.Context, conn database.Conn, entries []driversync.Entry) error {
	type record struct {
		driver [16]byte
		kind   string
		ref    [16]byte
	}
	last := map[record]int{}
	for i, e := range entries {
		last[record{e.DriverID.Bytes, e.Kind, e.RefID.Bytes}] = i
	}
	byDriver := map[[16]byte]*store.AddFeedEntriesParams{}
	for i, e := range entries {
		if last[record{e.DriverID.Bytes, e.Kind, e.RefID.Bytes}] != i || !e.DriverID.Valid {
			continue
		}
		p := byDriver[e.DriverID.Bytes]
		if p == nil {
			p = &store.AddFeedEntriesParams{DriverID: e.DriverID, TenantID: e.TenantID}
			byDriver[e.DriverID.Bytes] = p
		}
		p.Kinds = append(p.Kinds, e.Kind)
		p.RefIds = append(p.RefIds, e.RefID)
		p.Ops = append(p.Ops, e.Op)
		p.Reasons = append(p.Reasons, e.Reason)
	}
	drivers := make([][16]byte, 0, len(byDriver))
	for d := range byDriver {
		drivers = append(drivers, d)
	}
	slices.SortFunc(drivers, func(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) })
	q := store.New(conn)
	for _, d := range drivers {
		if err := q.AddFeedEntries(ctx, *byDriver[d]); err != nil {
			return fmt.Errorf("driver feed: %w", err)
		}
	}
	return nil
}

func (s *Service) Head(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID) (driversync.Head, error) {
	h, err := store.New(conn).GetFeedHead(ctx, store.GetFeedHeadParams{DriverID: driverID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return driversync.Head{}, nil
	}
	return driversync.Head{Seq: h.Seq, PrunedThrough: h.PrunedThrough}, err
}

func (s *Service) Since(ctx context.Context, conn database.Conn, tenantID, driverID pgtype.UUID, after int64, max int) ([]driversync.Item, error) {
	rows, err := store.New(conn).ListFeedEntries(ctx, store.ListFeedEntriesParams{
		DriverID: driverID, TenantID: tenantID, After: after, MaxEntries: int32(max),
	})
	if err != nil {
		return nil, err
	}
	items := make([]driversync.Item, len(rows))
	for i, r := range rows {
		items[i] = driversync.Item{Seq: r.Seq, Kind: r.Kind, RefID: r.RefID, Op: r.Op, Reason: r.Reason}
	}
	return items, nil
}

func (s *Service) Prune(ctx context.Context, before time.Time) (int64, error) {
	return s.q.PruneFeed(ctx, pgtype.Timestamptz{Time: before, Valid: true})
}
