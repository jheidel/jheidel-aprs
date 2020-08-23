package types

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/genproto/googleapis/type/latlng"
	"time"
)

type Credentials struct {
	ClientID     string
	ClientSecret string
	Token        *oauth2.Token
}

func (c *Credentials) Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scopes:       []string{"https://www.googleapis.com/auth/gmail.readonly"},
		Endpoint:     google.Endpoint,
		RedirectURL:  "https://where.jeffheidel.com/__/auth/handler",
	}
}

type Email struct {
	ID          string
	Message     string
	FullMessage string
	Time        time.Time
	Position    *latlng.LatLng
}
