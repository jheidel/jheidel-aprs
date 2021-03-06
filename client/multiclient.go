package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"
)

const (
	DedupHistory = time.Hour
)

// MultiClient implements a redundant connection to multiple clients.
type MultiClient struct {
	Clients []ClientInterface

	inbound chan *aprs.Packet
	history map[string]time.Time
}

func (c *MultiClient) init() {
	c.inbound = make(chan *aprs.Packet)
	c.history = make(map[string]time.Time)
}

func (c *MultiClient) cleanup() {
	close(c.inbound)
}

func (c *MultiClient) isDuplicate(p *aprs.Packet) bool {
	// Clean history
	for key, t := range c.history {
		if time.Since(t) > DedupHistory {
			delete(c.history, key)
		}
	}

	// Check history and track.
	key := p.Hash()
	_, ok := c.history[key]
	c.history[key] = time.Now()
	return ok
}

func (c *MultiClient) Run(ctx context.Context, wg *sync.WaitGroup) {
	c.init()

	log.Infof("Connecting on %d concurrent channels", len(c.Clients))

	cwg := &sync.WaitGroup{}
	for _, client := range c.Clients {
		client.Run(ctx, cwg)
		// Slightly stagger connection times to avoid addresses all resolving to
		// the same server
		sleep(ctx, time.Second)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer c.cleanup()

		recvc := make(chan *aprs.Packet)
		for _, cl := range c.Clients {
			go func(cl ClientInterface) {
				for p := range cl.Receive() {
					recvc <- p
				}
			}(cl)
		}

		for {
			select {
			case <-ctx.Done():
				cwg.Wait()
				return
			case p := <-recvc:
				if !c.isDuplicate(p) {
					c.inbound <- p
				} else {
					log.Debugf("Dropped duplicate packet")
				}
			}
		}
	}()
}

func (c *MultiClient) Receive() <-chan *aprs.Packet {
	return c.inbound
}

func (c *MultiClient) Status() error {
	var err error
	for _, sc := range c.Clients {
		err = sc.Status()
		if err == nil {
			return nil // one client healthy means we're healthy
		}
	}
	return fmt.Errorf("no clients healthy: %v", err)
}
