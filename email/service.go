package email

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"jheidel-aprs/email/types"
	"jheidel-aprs/firebase"
)

const (
	PollInterval = 10 * time.Second
	UserID       = "inreach@jeffheidel.com"

	CacheTTL = 7 * 24 * time.Hour
)

type Service struct {
	Auth     *Auth
	Firebase *firebase.Firebase

	ms    *gmail.UsersMessagesService
	cache map[string]time.Time
}

func (s *Service) fetchMail(ctx context.Context, ID string) (*types.Email, error) {
	m, err := s.ms.Get(UserID, ID).Do()
	if err != nil {
		return nil, err
	}

	text, err := toPlainText(m)
	if err != nil {
		return nil, err
	}

	log.Debugf("Got mail--\n%s", text)

	coords, err := extractCoords(text)
	if err != nil {
		log.Warnf("Failed to extract coords: %v", err)
	}

	e := &types.Email{
		ID:          m.Id,
		Time:        time.Unix(m.InternalDate/1000, 0),
		FullMessage: text,
		Message:     toMessage(text),
		Position:    coords,
	}
	return e, nil
}

func (s *Service) cleanCache() {
	for k, v := range s.cache {
		if time.Since(v) > CacheTTL {
			delete(s.cache, k)
		}
	}
}

func (s *Service) runOnce(ctx context.Context) error {
	// Connect to service if not already connected
	if s.ms == nil {
		log.Debugf("Connecting to gmail service")
		ts, err := s.Auth.TokenSource(ctx)
		if err != nil {
			return err
		}
		gs, err := gmail.NewService(ctx, option.WithTokenSource(ts))
		if err != nil {
			return err
		}
		s.ms = gmail.NewUsersMessagesService(gs)
	}

	log.Debugf("Checking for email messages")

	list, err := s.ms.List(UserID).Q("label:inreach newer_than:1d").Do()
	if err != nil {
		log.Fatal(err)
	}

	for _, m := range list.Messages {
		if _, ok := s.cache[m.Id]; ok {
			continue // Already dealt with this one.
		}
		log.Debugf("Found email message ID %q", m.Id)

		mail, err := s.fetchMail(ctx, m.Id)
		if err != nil {
			return err
		}

		s.cleanCache()
		s.cache[m.Id] = time.Now()

		if err := s.Firebase.ReportEmail(ctx, mail); err != nil {
			log.Warnf("Failed to save email: %v", err)
		}
	}

	return nil
}

func (s *Service) Run(ctx context.Context, wg *sync.WaitGroup) {
	s.cache = make(map[string]time.Time)

	wg.Add(1)
	go func() {
		defer wg.Done()
		f := func() {
			if err := s.runOnce(ctx); err != nil {
				log.Errorf("Failed email poll: %v", err)

				// Force reconnect next time around
				s.ms = nil
			}
		}
		f()
		t := time.NewTicker(PollInterval)
		for ctx.Err() == nil {
			select {
			case <-t.C:
				f()
			case <-ctx.Done():
			}
		}
	}()
}
