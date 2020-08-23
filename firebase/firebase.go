package firebase

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	fb "firebase.google.com/go"
	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/latlng"

	"jheidel-aprs/client"
	email "jheidel-aprs/email/types"
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
	health  sync.Map
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
		f := func() {
			if err := f.reportHealth(ctx); err != nil {
				log.Errorf("Failed to report health to firebase: %v", err)
			}
		}
		f()
		t := time.Tick(ReportHealthInterval)
		for ctx.Err() == nil {
			select {
			case <-t:
				f()
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

type EmailPacket struct {
	Raw string `firestore:"raw"`
}

type Packet struct {
	Hostname   string    `firestore:"hostname"`
	ReceivedAt time.Time `firestore:"received_at"`

	Message     string         `firestore:"message"`
	HasPosition bool           `firestore:"has_position"`
	Position    *latlng.LatLng `firestore:"position"`

	Aprs  *AprsPacket  `firestore:"aprs"`
	Email *EmailPacket `firestore:"email"`
}

func (f *Firebase) ReportAprsPacket(ctx context.Context, p *aprs.Packet) error {
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
	_, err := f.client.Collection("packets").Doc(id).Create(ctx, pkt)
	return err
}

func (f *Firebase) ReportEmail(ctx context.Context, e *email.Email) error {
	pkt := &Packet{
		Hostname:   hostname(),
		ReceivedAt: e.Time,
		Message:    e.Message,

		Email: &EmailPacket{
			Raw: e.FullMessage,
		},
	}
	if e.Position != nil {
		pkt.HasPosition = true
		pkt.Position = e.Position
	}
	id := fmt.Sprintf("email:%s", e.ID)
	_, err := f.client.Collection("packets").Doc(id).Create(ctx, pkt)
	return err
}

func (f *Firebase) ReportAprsAck(ctx context.Context, p *aprs.Packet, m *client.Message) error {
	_, err := f.client.Collection("packets").Doc(p.Hash()).Update(ctx, []firestore.Update{
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

type healthModule struct {
	Name    string `firestore:"name"`
	OK      bool   `firestore:"ok"`
	Message string `firestore:"message"`
}

type health struct {
	Hostname   string          `firestore:"hostname"`
	StartedAt  time.Time       `firestore:"started_at"`
	HealthyAt  time.Time       `firestore:"healthy_at"`
	BuildLabel string          `firestore:"build_label"`
	Modules    []*healthModule `firestore:"modules"`
}

func (f *Firebase) SetHealth(module string, err error) {
	h := &healthModule{
		Name: module,
		OK:   err == nil,
	}
	if err != nil {
		h.Message = err.Error()
	}
	log.Debugf("Module %q health %v", h.Name, h.OK)
	f.health.Store(module, h)
}

func (f *Firebase) reportHealth(ctx context.Context) error {
	h := &health{
		Hostname:   hostname(),
		StartedAt:  f.started,
		HealthyAt:  time.Now(),
		BuildLabel: f.BuildLabel,
	}
	f.health.Range(func(k, v interface{}) bool {
		h.Modules = append(h.Modules, v.(*healthModule))
		return true
	})
	_, err := f.client.Collection("gateways").Doc(h.Hostname).Set(ctx, h)
	return err
}

func (f *Firebase) StoreCredentials(ctx context.Context, creds *email.Credentials) error {
	_, err := f.client.Collection("environment").Doc("email_creds").Set(ctx, creds)
	return err
}

func (f *Firebase) LoadCredentials(ctx context.Context) (*email.Credentials, error) {
	var creds email.Credentials
	doc, err := f.client.Collection("environment").Doc("email_creds").Get(ctx)
	if err != nil {
		return nil, err
	}
	doc.DataTo(&creds)
	return &creds, nil
}
