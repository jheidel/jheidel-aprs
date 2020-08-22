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
	BuildLabel string

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

type AprsPacket struct {
	Raw       string `firestore:"raw"`
	Src       string `firestore:"src"`
	Dst       string `firestore:"dst"`
	Path      string `firestore:"path"`
	Comment   string `firestore:"comment"`
	MessageTo string `firestore:"message_to"`

	ReplyMessage    string    `firestore:"reply_message"`
	ReplySentAt     time.Time `firestore:"reply_sent_at"`
	ReplyLastSentAt time.Time `firestore:"reply_last_sent_at"`
	ReplyReceived   bool      `firestore:"reply_received"`
	ReplyReceivedAt time.Time `firestore:"reply_received_at"`
	ReplyID         int       `firestore:"reply_id"`
	ReplyAttempts   int       `firestore:"reply_attempts"`
}

type Packet struct {
	Hostname   string    `firestore:"hostname"`
	ReceivedAt time.Time `firestore:"received_at"`

	Message     string         `firestore:"message"`
	HasPosition bool           `firestore:"has_position"`
	Position    *latlng.LatLng `firestore:"position"`

	Aprs *AprsPacket `firestore:"aprs"`
}

func (f *Firebase) ReportPacket(ctx context.Context, p *aprs.Packet) error {
	pkt := &Packet{
		Hostname:   hostname(),
		ReceivedAt: time.Now(),
		Message:    p.Message,

		Aprs: &AprsPacket{
			Raw:     p.Raw,
			Src:     p.Src.String(),
			Dst:     p.Dst.String(),
			Path:    p.Path.String(),
			Comment: p.Comment,
		},
	}
	if p.MessageTo != nil {
		pkt.Aprs.MessageTo = p.MessageTo.String()
	}
	if p.Position != nil {
		pkt.HasPosition = true
		pkt.Position = &latlng.LatLng{
			Latitude:  p.Position.Latitude,
			Longitude: p.Position.Longitude,
		}
	}
	id := fmt.Sprintf("aprs:%s", p.Hash())
	// https://godoc.org/cloud.google.com/go/firestore
	_, err := f.client.Collection("aprs_packets").Doc(id).Create(ctx, pkt)
	return err
}

func (f *Firebase) Ack(ctx context.Context, p *aprs.Packet, m *client.Message) error {
	_, err := f.client.Collection("aprs_packets").Doc(p.Hash()).Update(ctx, []firestore.Update{
		{Path: "aprs.reply_message", Value: m.Message},
		{Path: "aprs.reply_sent_at", Value: m.SentAt},
		{Path: "aprs.reply_last_sent_at", Value: m.LastSentAt},
		{Path: "aprs.reply_received", Value: m.Received},
		{Path: "aprs.reply_received_at", Value: m.ReceivedAt},
		{Path: "aprs.reply_id", Value: m.ID},
		{Path: "aprs.reply_attempts", Value: m.Attempts},
	})
	return err
}

type fbAprsGateway struct {
	Hostname   string    `firestore:"hostname"`
	StartedAt  time.Time `firestore:"started_at"`
	HealthyAt  time.Time `firestore:"healthy_at"`
	BuildLabel string    `firestore:"build_label"`
}

func (f *Firebase) reportHealth(ctx context.Context) error {
	h := &fbAprsGateway{
		Hostname:   hostname(),
		StartedAt:  f.started,
		HealthyAt:  time.Now(),
		BuildLabel: f.BuildLabel,
	}
	_, err := f.client.Collection("aprs_gateways").Doc(h.Hostname).Set(ctx, h)
	return err
}
