package client

import (
	"context"
	"sync"
	"time"

	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"
)

const (
	IDResetInterval = 48 * time.Hour

	AttemptInterval = 30 * time.Second
	MaxAttempts     = 5
)

type Message struct {
	Addr          *aprs.Address
	Message       string
	SentAt        time.Time
	LastSentAt    time.Time
	NextAttemptAt time.Time
	ReceivedAt    time.Time
	Received      bool
	ID            int
	Attempts      int

	donec chan bool
}

func (m *Message) Wait() {
	<-m.donec
}

type Outbox struct {
	outbox map[int]*Message
	idGen  int

	ackc        chan int
	sendc, outc chan *Message
}

func (o *Outbox) attemptMessage(msg *Message) {
	msg.LastSentAt = time.Now()
	msg.NextAttemptAt = msg.LastSentAt.Add(AttemptInterval)
	if msg.Attempts >= MaxAttempts {
		msg.donec <- true
		close(msg.donec)
		msg.NextAttemptAt = time.Time{}
		log.Warnf("Exceeded retry count for message ID#%d, discarding", msg.ID)
		delete(o.outbox, msg.ID)
		return
	}
	msg.Attempts += 1
	log.Infof("Sending message ID#%d (attempt %d)", msg.ID, msg.Attempts)
	o.outc <- msg
}

func (o *Outbox) nextCheck() time.Duration {
	if len(o.outbox) == 0 {
		return time.Minute
	}
	var msg *Message
	for _, m := range o.outbox {
		if msg == nil || msg.NextAttemptAt.After(m.NextAttemptAt) {
			msg = m
		}
	}
	return msg.NextAttemptAt.Sub(time.Now()) + time.Millisecond
}

func (o *Outbox) loop(ctx context.Context) {
	idReset := time.NewTimer(IDResetInterval)
	nextCheck := time.NewTimer(time.Minute)
	for {
		nextCheck.Reset(o.nextCheck())
		select {
		case <-ctx.Done():
			return
		case msg := <-o.sendc:
			idReset.Reset(IDResetInterval)
			msg.ID = o.idGen
			o.idGen += 1
			o.outbox[msg.ID] = msg
			msg.SentAt = time.Now()
			o.attemptMessage(msg)

		case an := <-o.ackc:
			msg, ok := o.outbox[an]
			if !ok {
				continue // Message already received probably.
			}
			msg.Received = true
			msg.ReceivedAt = time.Now()
			msg.NextAttemptAt = time.Time{}
			msg.donec <- true
			log.Infof("Acknowledged message ID#%d", msg.ID)
			delete(o.outbox, msg.ID)

		case <-nextCheck.C:
			for _, msg := range o.outbox {
				if !msg.NextAttemptAt.After(time.Now()) {
					o.attemptMessage(msg)
				}
			}

		case <-idReset.C:
			o.idGen = 1
		}
	}
}

func (o *Outbox) Run(ctx context.Context, wg *sync.WaitGroup) {
	o.ackc = make(chan int)
	o.sendc = make(chan *Message)
	o.outc = make(chan *Message)
	o.outbox = make(map[int]*Message)
	o.idGen = 1

	wg.Add(1)
	go func() {
		defer wg.Done()
		o.loop(ctx)
	}()
}

func (o *Outbox) Ack(id int) {
	o.ackc <- id
}

func (o *Outbox) Outbound() <-chan *Message {
	return o.outc
}

func (o *Outbox) Send(addr *aprs.Address, message string) *Message {
	m := &Message{
		Addr:    addr,
		Message: message,
		donec:   make(chan bool, 1),
	}
	o.sendc <- m
	return m
}
