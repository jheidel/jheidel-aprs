package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/jheidel/go-aprs"
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

	// WARNING: responses from this server will be transmitted over ham radio
	// frequencies. Licensed HAM operators only!
	respond = flag.Bool("respond", false, "Whether to respond to beacon packets")

	logFile = flag.String("log_file", "log.txt", "File for logging packets")
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

func wait() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	fmt.Println()
	fmt.Println(sig)
}

func listen() error {

	// TODO: multi-channel for reliability, reconnections & deduping layer.

	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", *aprsAddr, *aprsPort))
	if err != nil {
		return err
	}
	defer conn.Close()

	if *respond {
		fmt.Println("Responses enabled!")
	}

	fmt.Println("Connection established", conn)

	lastSeen := time.Now()

	reader := bufio.NewReader(conn)

	call, err := aprs.ParseAddress(*serverCallsign)
	if err != nil {
		return err
	}
	pass := call.Secret()
	fmt.Printf("Computed password %d\n", pass)

	fmt.Fprintf(conn, "user %s pass %d vers %s %s filter %s\n",
		*serverCallsign, pass, ClientName, ClientVersion, *filterCallsign)

	// message number counter
	// TODO reset this periodically after long periods?
	n := 1

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for t := range ticker.C {
			fmt.Fprintf(conn, "# %s keepalive %s\n", ClientName, t)
		}
	}()

	go func() {
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("Receive error:", err)
				return
			}
			lastSeen = time.Now()
			msg = strings.TrimSpace(msg)

			if strings.HasPrefix(msg, "#") {
				log.Printf("Comment: %v\n", msg)
				continue
			}

			if err := logPacket(msg); err != nil {
				log.Printf("Failed to log packet: %v\n", err)
			}

			p, err := aprs.ParsePacket(msg)
			if err != nil {
				log.Printf("Invalid packet: %v: %v\n", msg, err)
				continue
			}

			log.Printf("NEW PACKET:\n%v\n", spew.Sdump(p))

			if p.IsAck() {
				an, err := p.AckNumber()
				if err != nil {
					log.Printf("Failed to get ack: %v", err)
					continue
				}
				log.Printf("Previous message #%d acknowledged.", an)
				continue
			}

			log.Printf("POSITION: %v\n", p.Position.String())

			if *respond {
				now := time.Now()
				txmsg := fmt.Sprintf("rx at %s", now.Format("3:04 PM"))
				resp := fmt.Sprintf("%s>APRS,WIDE::%s : %s{%d\n", *serverCallsign, p.Src.String(), txmsg, n)
				log.Printf("Sending response (msg #%d): %q\n", n, strings.TrimSpace(resp))
				if _, err := conn.Write([]byte(resp)); err != nil {
					log.Printf("Failed to write packet %v\n", err)
				}

				n += 1
			}
		}
	}()

	wait()
	return nil
}

func main() {
	flag.Parse()
	fmt.Println("aprs listen server start")

	err := listen()
	if err != nil {
		fmt.Println("error; ", err)
	}
}
