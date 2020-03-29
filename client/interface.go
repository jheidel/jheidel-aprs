package client

import (
	"context"
	"sync"

	"github.com/jheidel/go-aprs"
)

type ClientInterface interface {
	Run(ctx context.Context, wg *sync.WaitGroup)
	Receive() <-chan *aprs.Packet
}
