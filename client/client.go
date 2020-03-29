package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jheidel/go-aprs"
	log "github.com/sirupsen/logrus"
)

const (
	ClientName    = "jheidel-aprs"
	ClientVersion = "1.1"

	ReconnectDelayMin = 500 * time.Millisecond
	ReconnectDelayMax = 30 * time.Second
	ReconnectExp      = 1.4

	KeepAliveInterval = 30 * time.Second
	ConnectionTimeout = 2 * time.Minute
)

type Client struct {
	Callsign      string
	Filter        string
	ServerAddress string
	ServerPort    int
	Outbox        *Outbox

	reconnectDelay time.Duration
	inbound        chan *aprs.Packet

	packetIndex int
}

func (c *Client) oneConnection(ctx context.Context) error {
	// Connect to APRS-IS
	dialer := &net.Dialer{
		Timeout: ConnectionTimeout,
		Resolver: &net.Resolver{
			PreferGo:     true,
			StrictErrors: true,
		},
	}
	conn, err := dialer.Dial("tcp", fmt.Sprintf("%s:%d", c.ServerAddress, c.ServerPort))
	if err != nil {
		return err
	}
	defer conn.Close()

	clog := log.WithFields(log.Fields{
		"remote": conn.RemoteAddr().String(),
	})
	clog.Infof("Connection established")

	// Auth with server
	call, err := aprs.ParseAddress(c.Callsign)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(conn, "user %s pass %d vers %s %s filter %s\n",
		c.Callsign, call.Secret(), ClientName, ClientVersion, c.Filter)
	if err != nil {
		return err
	}

	// Read from connection async.
	reader := bufio.NewReader(conn)
	readc := make(chan string)
	errc := make(chan error)
	go func() {
		defer close(readc)
		defer close(errc)
		for ctx.Err() == nil {
			s, err := reader.ReadString('\n')
			if err != nil {
				errc <- err
				return
			}
			readc <- strings.TrimSpace(s)
		}
	}()

	keepAliveTx := time.NewTicker(KeepAliveInterval)
	keepAliveRx := time.NewTimer(ConnectionTimeout)
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-keepAliveTx.C:
			if _, err := fmt.Fprintf(conn, "# %s keepalive %s\n", ClientName, t); err != nil {
				return fmt.Errorf("keepalive transmit failed: %v", err)
			}
		case <-keepAliveRx.C:
			return errors.New("timed out waiting for keepalive from server")
		case err := <-errc:
			return fmt.Errorf("receive error: %v", err)
		case line := <-readc:
			// New line received from server.
			keepAliveRx.Reset(ConnectionTimeout)
			// Reset exponential backoff delay.
			c.reconnectDelay = ReconnectDelayMin

			// Log, but ignore comments.
			if strings.HasPrefix(line, "#") {
				clog.Debugf("Server comment: %v", line)
				continue
			}

			p, err := aprs.ParsePacket(line)
			if err != nil {
				clog.Errorf("Ignored invalid packet: %v: %v", line, err)
				continue
			}

			clog.Debugf("RECEIVE: %v", p.Raw)

			if p.Src.String() == c.Callsign {
				// We may pick up our own packets due to multiple APRS-IS connections.
				clog.Debugf("Ignored our own packet")
				continue
			}
			if p.MessageTo != nil && p.MessageTo.String() != c.Callsign {
				clog.Debugf("Message to %q is not intended for us, dropped", p.MessageTo.String())
				continue
			}

			if p.IsAck() {
				an, err := p.AckNumber()
				if err != nil {
					clog.Errorf("Failed to get ack: %v", err)
					continue
				}
				clog.Debugf("Ack packet for message #%d", an)
				c.Outbox.Ack(an)
				continue
			}

			// Pass along to listener.
			c.inbound <- &p

		case msg := <-c.Outbox.Outbound():
			line := fmt.Sprintf("%s>APRS,WIDE::%s : %s{%d\n", c.Callsign, msg.Addr.String(), msg.Message, msg.ID)

			clog.Debugf("SEND: %v", strings.TrimSpace(line))
			if _, err := conn.Write([]byte(line)); err != nil {
				return fmt.Errorf("packet write: %v", err)
			}
		}
	}

	return errors.New("unexpected!")
}

func (c *Client) loop(ctx context.Context) {
	err := c.oneConnection(ctx)
	if err == nil {
		log.Infof("Disconnected from server")
		return
	}
	log.Errorf("Disconnected from server: %v", err)

	log.Infof("Reconnecting after delay of %v...", c.reconnectDelay)
	sleep(ctx, c.reconnectDelay)

	// Increment exponential backoff for next time.
	c.reconnectDelay = time.Duration(ReconnectExp*float64(c.reconnectDelay/time.Millisecond)) * time.Millisecond
	if c.reconnectDelay > ReconnectDelayMax {
		c.reconnectDelay = ReconnectDelayMax
	}
}

func (c *Client) init() {
	c.inbound = make(chan *aprs.Packet)
}

func (c *Client) cleanup() {
	close(c.inbound)
}

func (c *Client) Run(ctx context.Context, wg *sync.WaitGroup) {
	c.init()
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer c.cleanup()
		for ctx.Err() == nil {
			c.loop(ctx)
		}
	}()
}

func (c *Client) Receive() <-chan *aprs.Packet {
	return c.inbound
}
