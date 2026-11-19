//go:build integration

package integration

// The app with RabbitMQ (docs/architecture.md, section 6): GPS fixes go
// through tracking.ingest, live updates cross instances on kiflet.live,
// and shipment events leave event_outbox for kiflet.events.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/kiflet/backend/internal/service"
	"github.com/kiflet/backend/pkg/broker"
)

// rabbitImage is the version deployments/ runs.
const rabbitImage = "rabbitmq:4.1-management-alpine"

// sharedRabbitMQ starts one RabbitMQ for the tests that need one; TestMain
// stops it.
var sharedRabbitMQ = sync.OnceValues(startRabbitMQ)

var stopRabbitMQ = func() {}

func startRabbitMQ() (string, error) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        rabbitImage,
			Env:          map[string]string{"RABBITMQ_DEFAULT_USER": "kiflet", "RABBITMQ_DEFAULT_PASS": "kiflet"},
			ExposedPorts: []string{"5672/tcp"},
			WaitingFor:   wait.ForLog("Server startup complete").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		return "", err
	}
	stopRabbitMQ = func() { _ = c.Terminate(ctx, testcontainers.StopTimeout(time.Second)) }
	endpoint, err := c.PortEndpoint(ctx, "5672/tcp", "")
	if err != nil {
		return "", err
	}
	return "amqp://kiflet:kiflet@" + endpoint + "/", nil
}

func rabbitURL(t *testing.T) string {
	t.Helper()
	url, err := sharedRabbitMQ()
	if err != nil {
		t.Fatal(err)
	}
	return url
}

// withRabbitMQ runs the app with RabbitMQ, every worker on its queue.
func withRabbitMQ(url string) func(*setup) {
	return func(s *setup) {
		s.cfg.RabbitMQURL = url
		s.cfg.WorkerTracking = "queue"
		s.cfg.WorkerLive = "queue"
	}
}

// eventually fails the test if ok isn't true within ten seconds.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("after 10 s: %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestRabbitMQ(t *testing.T) {
	url := rabbitURL(t)
	first, second := newEnv(t, withRabbitMQ(url)), newEnv(t, withRabbitMQ(url))
	office := first.company("Rabbit Freight")
	other := first.company("Other Rabbit Freight")

	// Something listening to kiflet.events, as the consumers to come will.
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // closing
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(q.Name, "shipment.#", broker.Events, false, nil); err != nil {
		t.Fatal(err)
	}
	events, err := ch.Consume(q.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("live updates cross instances", func(t *testing.T) {
		mine, theirs := office.live(), other.live()
		time.Sleep(300 * time.Millisecond) // the hub registers both
		id, _ := office.on(second).book("Addis Ababa", "Adama", "")
		want := "shipment " + id
		deadline := time.After(5 * time.Second)
		for got := ""; got != want; {
			select {
			case got = <-mine:
			case <-deadline:
				t.Fatalf("the dashboard on the first instance had no %q from the second", want)
			}
		}
		select {
		case m := <-theirs:
			t.Errorf("another company's dashboard was sent %q", m)
		case <-time.After(300 * time.Millisecond):
		}
	})

	driverID, phone, pin := office.addDriver("Rabbit Driver")
	shipmentID, tracking := office.book("Addis Ababa", "Adama", driverID)

	t.Run("shipment events reach kiflet.events", func(t *testing.T) {
		deadline := time.After(10 * time.Second)
		for {
			select {
			case d := <-events:
				var env broker.Envelope
				if err := json.Unmarshal(d.Body, &env); err != nil {
					t.Fatalf("not an envelope: %s", d.Body)
				}
				var m service.ShipmentEventMessage
				_ = json.Unmarshal(env.Payload, &m)
				if m.ShipmentID != shipmentID || env.Type != "shipment.created" {
					continue
				}
				if d.RoutingKey != "shipment.created" || env.Version != 1 || env.TenantID == "" || m.TrackingNumber != tracking || d.MessageId != env.ID {
					t.Errorf("event %s %+v %+v", d.RoutingKey, env, m)
				}
				return
			case <-deadline:
				t.Fatal("no shipment.created for the new shipment")
			}
		}
	})
	eventually(t, "the shipment's events marked sent", func() bool {
		return scalar[int64](t, `SELECT count(*) FROM event_outbox WHERE payload->>'shipment_id' = $1 AND sent_at IS NULL`, shipmentID) == 0 &&
			scalar[int64](t, `SELECT count(*) FROM event_outbox WHERE payload->>'shipment_id' = $1`, shipmentID) > 0
	})

	t.Run("positions through tracking.ingest", func(t *testing.T) {
		d := first.signIn(phone, pin)
		if r := d.status(clientID(), shipmentID, "in_transit"); !r.OK {
			t.Fatalf("depart: %+v", r)
		}
		now := time.Now()
		var locs []service.SyncLocation
		for i := range 30 {
			locs = append(locs, service.SyncLocation{
				ClientID: clientID(), ShipmentID: shipmentID, Lat: 9.0 + float64(i)*0.001, Lng: 38.75,
				RecordedAt: now.Add(time.Duration(i-30) * time.Minute),
			})
		}
		// One that can't be real, refused before the queue.
		locs = append(locs, service.SyncLocation{ClientID: clientID(), ShipmentID: shipmentID, Lat: 51.5, Lng: -0.1, RecordedAt: now})
		resp, code := d.sync(service.SyncRequest{Locations: locs})
		if code != 200 || resp.LocationsStored != 30 {
			t.Fatalf("sync: %d, %d stored, want 30", code, resp.LocationsStored)
		}
		eventually(t, "30 positions in tracking_events", func() bool {
			return scalar[int64](t, `SELECT count(*) FROM tracking_events WHERE shipment_id = $1`, shipmentID) == 30
		})
		eventually(t, "the driver's newest position kept", func() bool {
			return scalar[bool](t, `SELECT EXISTS (SELECT 1 FROM driver_positions WHERE driver_id = $1 AND round(ST_Y(coordinates::geometry)::numeric, 3) = 9.029)`, driverID)
		})
		// The phone sends them again, not having heard back: nothing doubles.
		if _, code := d.sync(service.SyncRequest{Locations: locs[:30]}); code != 200 {
			t.Fatalf("sync again: %d", code)
		}
		time.Sleep(time.Second)
		if n := scalar[int64](t, `SELECT count(*) FROM tracking_events WHERE shipment_id = $1`, shipmentID); n != 30 {
			t.Errorf("%d positions after the repeat, want 30", n)
		}
	})
}

// TestBrokerRetries: a message whose handler fails is tried again after
// its queue's delay, and parked in the dead-letter queue after its last
// attempt; one that isn't an envelope is parked at once.
func TestBrokerRetries(t *testing.T) {
	url := rabbitURL(t)
	queue := broker.Queue{
		Name: "test.flaky", Bindings: []broker.Binding{{Exchange: broker.Commands, Key: "test.flaky"}},
		RetryAfter: 200 * time.Millisecond, MaxAttempts: 3,
	}
	broker.Topology = append(broker.Topology, queue)
	t.Cleanup(func() { broker.Topology = broker.Topology[:len(broker.Topology)-1] })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b, err := broker.Dial(ctx, url, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	var mu sync.Mutex
	tries := map[string][]int{}
	var handled atomic.Int32
	go b.Consume(ctx, queue.Name, 10, func(_ context.Context, batch []broker.Delivery) error {
		mu.Lock()
		defer mu.Unlock()
		var fail bool
		for _, d := range batch {
			var p struct{ Fail bool }
			_ = json.Unmarshal(d.Envelope.Payload, &p)
			tries[d.Envelope.ID] = append(tries[d.Envelope.ID], d.Attempt)
			fail = fail || p.Fail
		}
		if fail {
			return errors.New("refused")
		}
		handled.Add(int32(len(batch)))
		return nil
	})

	good, _ := broker.NewEnvelope("test.flaky", "", map[string]bool{"Fail": false})
	bad, _ := broker.NewEnvelope("test.flaky", "", map[string]bool{"Fail": true})
	if err := b.Publish(ctx, broker.Message{Exchange: broker.Commands, RoutingKey: "test.flaky", Envelope: good}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the good message handled", func() bool { return handled.Load() == 1 })
	if err := b.Publish(ctx, broker.Message{Exchange: broker.Commands, RoutingKey: "test.flaky", Envelope: bad}); err != nil {
		t.Fatal(err)
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // closing
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	// Not an envelope.
	if err := ch.Publish(broker.Commands, "test.flaky", false, false, amqp.Publishing{Body: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	dead := func() int {
		q, err := ch.QueueDeclarePassive("test.flaky.dead", true, false, false, false, amqp.Table{"x-queue-type": "quorum"})
		if err != nil {
			t.Fatal(err)
		}
		return q.Messages
	}
	eventually(t, "two messages parked", func() bool { return dead() == 2 })
	mu.Lock()
	if got := fmt.Sprint(tries[bad.ID]); got != "[1 2 3]" {
		t.Errorf("the failing message's attempts: %s, want [1 2 3]", got)
	}
	mu.Unlock()
	d, ok, err := ch.Get("test.flaky.dead", true)
	for ok && err == nil && d.MessageId != bad.ID {
		d, ok, err = ch.Get("test.flaky.dead", true)
	}
	if !ok || d.Headers["x-kiflet-error"] != "refused" {
		t.Errorf("parked message %q, headers %v", d.MessageId, d.Headers)
	}
	_, _ = ch.QueuePurge("test.flaky.dead", false)
}
