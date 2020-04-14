package firebase

import (
	"context"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	fb "firebase.google.com/go"
	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/latlng"

	"jheidel-aprs/client"
)

const (
	ReportHealthInterval = time.Minute
)

func hostname() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}
	h, _ := os.Hostname()
	return h
}

type Firebase struct {
	client  *firestore.Client
	started time.Time
}

func New(ctx context.Context, credentials string) (*Firebase, error) {
	opt := option.WithCredentialsFile(credentials)
	app, err := fb.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil, fmt.Errorf("firestore app: %v", err)
	}
	client, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("firestore client: %v", err)
	}
	log.Infof("Connected to firestore")
	f := &Firebase{
		client:  client,
		started: time.Now(),
	}
	go func() {
		t := time.Tick(ReportHealthInterval)
		for ctx.Err() == nil {
			if err := f.reportHealth(ctx); err != nil {
				log.Errorf("Failed to report health to firebase: %v", err)
			}
			select {
			case <-t:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()
	return f, nil
}

type fbAprsPacket struct {
	Hostname   string    `firestore:"hostname"`
	ReceivedAt time.Time `firestore:"received_at"`

	Raw         string         `firestore:"raw"`
	Src         string         `firestore:"src"`
	Dst         string         `firestore:"dst"`
	Path        string         `firestore:"path"`
	Comment     string         `firestore:"comment"`
	Message     string         `firestore:"message"`
	MessageTo   string         `firestore:"message_to"`
	HasPosition bool           `firestore:"has_position"`
	Position    *latlng.LatLng `firestore:"position"`

	ReplyMessage    string    `firestore:"reply_message"`
	ReplySentAt     time.Time `firestore:"reply_sent_at"`
	ReplyLastSentAt time.Time `firestore:"reply_last_sent_at"`
	ReplyReceived   bool      `firestore:"reply_received"`
	ReplyReceivedAt time.Time `firestore:"reply_received_at"`
	ReplyID         int       `firestore:"reply_id"`
	ReplyAttempts   int       `firestore:"reply_attempts"`
}

func (f *Firebase) ReportPacket(ctx context.Context, p *aprs.Packet) error {
	pkt := &fbAprsPacket{
		Hostname:   hostname(),
		ReceivedAt: time.Now(),

		Raw:     p.Raw,
		Src:     p.Src.String(),
		Dst:     p.Dst.String(),
		Path:    p.Path.String(),
		Comment: p.Comment,
		Message: p.Message,
	}
	if p.MessageTo != nil {
		pkt.MessageTo = p.MessageTo.String()
	}
	if p.Position != nil {
		pkt.HasPosition = true
		pkt.Position = &latlng.LatLng{
			Latitude:  p.Position.Latitude,
			Longitude: p.Position.Longitude,
		}
	}
	// https://godoc.org/cloud.google.com/go/firestore
	_, err := f.client.Collection("aprs_packets").Doc(p.Hash()).Create(ctx, pkt)
	return err
}

func (f *Firebase) Ack(ctx context.Context, p *aprs.Packet, m *client.Message) error {
	_, err := f.client.Collection("aprs_packets").Doc(p.Hash()).Update(ctx, []firestore.Update{
		{Path: "reply_message", Value: m.Message},
		{Path: "reply_sent_at", Value: m.SentAt},
		{Path: "reply_last_sent_at", Value: m.LastSentAt},
		{Path: "reply_received", Value: m.Received},
		{Path: "reply_received_at", Value: m.ReceivedAt},
		{Path: "reply_id", Value: m.ID},
		{Path: "reply_attempts", Value: m.Attempts},
	})
	return err
}

type fbAprsGateway struct {
	Hostname  string    `firestore:"hostname"`
	StartedAt time.Time `firestore:"started_at"`
	HealthyAt time.Time `firestore:"healthy_at"`
}

func (f *Firebase) reportHealth(ctx context.Context) error {
	h := &fbAprsGateway{
		Hostname:  hostname(),
		StartedAt: f.started,
		HealthyAt: time.Now(),
	}
	_, err := f.client.Collection("aprs_gateways").Doc(h.Hostname).Set(ctx, h)
	return err
}
