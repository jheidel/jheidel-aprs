package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	aprsAddr = "rotate.aprs2.net"
	aprsPort = 14580
)

func wait() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigs
	fmt.Println()
	fmt.Println(sig)
}

func listen() error {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", aprsAddr, aprsPort))
	if err != nil {
		return err
	}
	defer conn.Close()

	fmt.Println("Connection established", conn)

	reader := bufio.NewReader(conn)

	login := "user KI7QIV-R pass -1 vers jhclient 1.0 filter p/KI7QIV\n"
	fmt.Fprintf(conn, login)

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for t := range ticker.C {
			fmt.Fprintf(conn, "# jhclient keepalive %s\n", t)
		}
	}()

	go func() {
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("Receive error:", err)
				return
			}
			fmt.Println("Message: ", msg)

			if !strings.HasPrefix(msg, "#") {
				continue
			}

			// TODO parse packet
		}
	}()

	wait()
	return nil
}

func main() {
	fmt.Println("aprs listen server start")

	err := listen()
	if err != nil {
		fmt.Println("error; ", err)
	}
}
