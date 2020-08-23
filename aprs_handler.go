package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"

	"jheidel-aprs/client"
	"jheidel-aprs/firebase"
)

type AprsHandler struct {
	Client   client.ClientInterface
	Outbox   *client.Outbox
	Firebase *firebase.Firebase
}

func (h *AprsHandler) Run(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()

		for ctx.Err() == nil {
			p := <-h.Client.Receive()
			if p == nil {
				break
			}

			log.Debugf("Received packet:\n%v", spew.Sdump(p))
			log.Infof("MESSAGE: %v", p.Message)
			log.Infof("POSITION: %v", p.Position.String())

			if err := h.Firebase.ReportAprsPacket(ctx, p); err != nil {
				// This might be an error reporting, but there might also be another
				// instance that reported first before we could.
				log.Warnf("Failed to report packet to firebase: %v", err)
				continue
			}

			if *respond {
				now := time.Now()
				text := fmt.Sprintf("RX %s", now.Format("3:04 PM"))

				// TODO: generate reply message from firebase, maybe using pending
				// messages?

				log.Infof("REPLY: %v", text)
				msg := h.Outbox.Send(p.Src, text)
				go func(p *aprs.Packet) {
					// Wait for acknowledgement or timeout.
					msg.Wait()
					log.Infof("Message done %v", spew.Sdump(msg))
					if err := h.Firebase.ReportAprsAck(ctx, p, msg); err != nil {
						log.Errorf("Failed to report message completion to firebase; %v", err)
					}
				}(p)
			}
		}
	}()
}
