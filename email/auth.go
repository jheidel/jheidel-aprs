package email

import (
	"context"
	"fmt"

	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"jheidel-aprs/email/types"
	"jheidel-aprs/firebase"
)

type Auth struct {
	Firebase *firebase.Firebase

	creds *types.Credentials
}

func (a *Auth) Generate(ctx context.Context) error {
	c := &types.Credentials{}
	log.Infof("Conducting auth key generation")

	fmt.Printf("Enter Client ID: ")
	if _, err := fmt.Scan(&c.ClientID); err != nil {
		return err
	}
	log.Infof("Accepted ID %q", c.ClientID)

	fmt.Printf("Enter Client Secret: ")
	if _, err := fmt.Scan(&c.ClientSecret); err != nil {
		return err
	}
	log.Infof("Accepted secret %q", c.ClientSecret)

	url := c.Config().AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Printf("Visit the URL for the auth dialog: %v\n", url)
	fmt.Printf("Enter access code: ")

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return err
	}

	log.Infof("Accepted code %q", code)
	var err error
	c.Token, err = c.Config().Exchange(ctx, code)
	if err != nil {
		return err
	}

	log.Infof("Storing token in firebase")
	if err := a.Firebase.StoreCredentials(ctx, c); err != nil {
		return err
	}

	log.Infof("Successful auth generation.")
	return nil
}

func (a *Auth) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if a.creds == nil {
		var err error
		a.creds, err = a.Firebase.LoadCredentials(ctx)
		if err != nil {
			return nil, err
		}
	}
	return a.creds.Config().TokenSource(ctx, a.creds.Token), nil
}
