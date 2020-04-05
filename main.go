package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"

	"github.com/jheidel/go-aprs"
	"jheidel-aprs/client"
	"jheidel-aprs/firebase"
)

const (
	ClientName    = "jheidel-aprs"
	ClientVersion = "1.0"
)

var (
	serverCallsign = flag.String("server_callsign", "KI7QIV-10", "Amateur radio callsign for the aprs server")

	filterCallsign = flag.String("filter_callsign", "p/KI7QIV", "APRS-IS filter to apply")

	aprsAddr = flag.String("aprs_addr", "rotate.aprs2.net", "Address of the APRS-IS server to use")
	aprsPort = flag.Int("aprs_port", 14580, "Port of the provide aprs_addr APRS-IS server")

	aprsChannels = flag.Int("aprs_channels", 3, "Number of concurrent channels to use for APRS-IS")

	// WARNING: responses from this server will be transmitted over ham radio
	// frequencies. Licensed HAM operators only!
	respond = flag.Bool("respond", false, "Whether to respond to beacon packets")

	logFile = flag.String("log_file", "log.txt", "File for logging packets")

	debug = flag.Bool("debug", false, "Log at debug verbosity")

	credentials = flag.String("credentials", "/etc/jheidel-aprs/key.json", "Location of firebase auth key")
)

func logPacket(msg string) error {
	f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	now := time.Now().Format(time.RFC3339)
	if _, err := f.Write([]byte(now + ": " + msg + "\n")); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return nil
}

func topLevelContext() context.Context {
	ctx, cancelf := context.WithCancel(context.Background())
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigs
		log.Warnf("Caught signal %q, shutting down.", sig)
		cancelf()
	}()
	return ctx
}

func main() {
	flag.Parse()

	customFormatter := new(log.TextFormatter)
	customFormatter.TimestampFormat = "2006-01-02 15:04:05"
	customFormatter.FullTimestamp = true
	log.SetFormatter(customFormatter)
	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	log.Infof("jheidel-aprs server starting")

	if *respond {
		log.Warnf("Responses enabled, will transmit packets!")
	}

	ctx := topLevelContext()
	wg := &sync.WaitGroup{}

	fb, err := firebase.New(ctx, *credentials)
	if err != nil {
		log.Fatalf("Failed to initialize firebase: %v", err)
	}

	outbox := &client.Outbox{}
	outbox.Run(ctx, wg)

	single := &client.Client{
		Callsign:      *serverCallsign,
		Filter:        *filterCallsign,
		ServerAddress: *aprsAddr,
		ServerPort:    *aprsPort,
		Outbox:        outbox,
	}

	var conn client.ClientInterface
	multi := &client.MultiClient{}
	// Connect to multiple servers in parallel for increased reliability.
	for i := 0; i < *aprsChannels; i++ {
		next := &client.Client{}
		*next = *single
		multi.Clients = append(multi.Clients, next)
	}
	conn = multi

	// Initiate async connection to server(s)
	conn.Run(ctx, wg)

	for ctx.Err() == nil {
		p := <-conn.Receive()
		if p == nil {
			break
		}

		log.Debugf("Received packet:\n%v", spew.Sdump(p))

		if err := logPacket(p.Raw); err != nil {
			log.Errorf("Failed to log packet: %v", err)
		}

		log.Infof("MESSAGE: %v", p.Message)
		log.Infof("POSITION: %v", p.Position.String())

		if err := fb.ReportPacket(ctx, p); err != nil {
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
			msg := outbox.Send(p.Src, text)
			go func(p *aprs.Packet) {
				// Wait for acknowledgement or timeout.
				msg.Wait()
				log.Infof("Message done %v", spew.Sdump(msg))
				if err := fb.Ack(ctx, p, msg); err != nil {
					log.Errorf("Failed to report message completion to firebase; %v", err)
				}
			}(p)
		}
	}

	log.Infof("jheidel-aprs shutdown")
}
