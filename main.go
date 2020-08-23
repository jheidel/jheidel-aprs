package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"

	"jheidel-aprs/client"
	"jheidel-aprs/email"
	"jheidel-aprs/firebase"
)

var (
	serverCallsign = flag.String("server_callsign", "KI7QIV-10", "Amateur radio callsign for the aprs server")

	filterCallsign = flag.String("filter_callsign", "p/KI7QIV", "APRS-IS filter to apply")

	aprsAddr = flag.String("aprs_addr", getEnv("APRS_ADDR", "noam.aprs2.net"), "Address of the APRS-IS server to use")
	aprsPort = flag.Int("aprs_port", 14580, "Port of the provide aprs_addr APRS-IS server")

	aprsChannels = flag.Int("aprs_channels", getEnvInt("APRS_CHANNELS", 3), "Number of concurrent channels to use for APRS-IS")

	// WARNING: responses from this server will be transmitted over ham radio
	// frequencies. Licensed HAM operators only!
	respond = flag.Bool("respond", false, "Whether to respond to beacon packets")

	debug = flag.Bool("debug", false, "Log at debug verbosity")

	credentials = flag.String("credentials", "/etc/jheidel-aprs/key.json", "Location of firebase auth key")

	emailAuth = flag.Bool("email_auth", false, "Run email authorization")

	buildLabel string
)

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil {
			return i
		}
	}
	return defaultValue
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

	log.Infof("jheidel-aprs server starting (version %s)", buildLabel)

	if *respond {
		log.Warnf("Responses enabled, will transmit packets!")
	}

	ctx := topLevelContext()
	wg := &sync.WaitGroup{}

	fb, err := firebase.New(ctx, *credentials)
	if err != nil {
		log.Fatalf("Failed to initialize firebase: %v", err)
	}
	fb.BuildLabel = buildLabel

	eauth := &email.Auth{
		Firebase: fb,
	}

	if *emailAuth {
		if err := eauth.Generate(ctx); err != nil {
			log.Fatalf("Failed to generate email creds: %v", err)
		}
		log.Exit(0)
	}

	email := &email.Service{
		Auth:     eauth,
		Firebase: fb,
	}
	email.Run(ctx, wg)

	outbox := &client.Outbox{}
	outbox.Run(ctx, wg)

	single := &client.Client{
		Callsign:      *serverCallsign,
		Filter:        *filterCallsign,
		ServerAddress: *aprsAddr,
		ServerPort:    *aprsPort,
		Outbox:        outbox,
		BuildLabel:    buildLabel,
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

	ah := &AprsHandler{
		Client:   conn,
		Outbox:   outbox,
		Firebase: fb,
	}
	ah.Run(ctx, wg)

	// TODO implement email handler

	wg.Wait()
	log.Infof("jheidel-aprs shutdown")
}
