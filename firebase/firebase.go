package firebase

import (
	"context"
	"crypto/sha1"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	fb "firebase.google.com/go"
	"github.com/jheidel/go-aprs"
	"google.golang.org/api/option"

	"jheidel-aprs/client"
)

type Firebase struct {
	client *firestore.Client
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
	return &Firebase{client: client}, nil
}

type fbAprsPacket struct {
	Hostname   string    `firestore:"hostname"`
	ReceivedAt time.Time `firestore:"received_at"`

	Raw         string  `firestore:"raw"`
	Src         string  `firestore:"src"`
	Dst         string  `firestore:"dst"`
	Path        string  `firestore:"path"`
	Comment     string  `firestore:"comment"`
	Message     string  `firestore:"message"`
	MessageTo   string  `firestore:"message_to"`
	HasPosition bool    `firestore:"has_position"`
	Latitude    float64 `firestore:"latitude"`
	Longitude   float64 `firestore:"longitude"`

	ReplyMessage    string    `firestore:"reply_message"`
	ReplySentAt     time.Time `firestore:"reply_sent_at"`
	ReplyLastSentAt time.Time `firestore:"reply_last_sent_at"`
	ReplyReceived   bool      `firestore:"reply_received"`
	ReplyReceivedAt time.Time `firestore:"reply_received_at"`
	ReplyID         int       `firestore:"reply_id"`
	ReplyAttempts   int       `firestore:"reply_attempts"`
}

// ID provides a unique identifier for this packet for server-side deduplication.
func (p *fbAprsPacket) ID() string {
	h := sha1.New()
	h.Write([]byte(p.Raw))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (f *Firebase) ReportPacket(ctx context.Context, p *aprs.Packet) error {
	hst, _ := os.Hostname()
	pkt := &fbAprsPacket{
		Hostname:   hst,
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
		pkt.Latitude = p.Position.Latitude
		pkt.Longitude = p.Position.Longitude
	}
	// https://godoc.org/cloud.google.com/go/firestore
	_, err := f.client.Collection("aprs_packets").Doc(pkt.ID()).Create(ctx, pkt)
	return err
}

func (f *Firebase) Ack(ctx context.Context, p *aprs.Packet, m *client.Message) error {
	pkt := &fbAprsPacket{
		Raw: p.Raw,
	}
	_, err := f.client.Collection("aprs_packets").Doc(pkt.ID()).Update(ctx, []firestore.Update{
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
