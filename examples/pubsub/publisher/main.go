package main

import (
	"fmt"
	"github.com/loophole-labs/frisbee"
	pubsub "github.com/loophole-labs/frisbee/examples/pubsub/schema"
	"hash/crc32"
	"os"
	"os/signal"
	"time"
)

const PUB = uint32(1)

var topic = []byte("TOPIC 1")
var topicHash = crc32.ChecksumIEEE(topic)

func main() {
	exit := make(chan os.Signal)
	signal.Notify(exit, os.Interrupt)
	c := pubsub.NewClient("127.0.0.1:8192", &ClientHandler{})
	err := c.Connect()
	if err != nil {
		panic(err)
	}

	go func() {
		i := 0
		for {
			message := []byte(fmt.Sprintf("PUBLISHED MESSAGE: %d", i))
			err := c.Write(&frisbee.Message{
				From:          topicHash,
				To:            topicHash,
				Id:            uint32(i),
				Operation:     PUB,
				ContentLength: uint64(len(message)),
			}, &message)
			if err != nil {
				panic(err)
			}
			i++
			time.Sleep(time.Second)
		}
	}()

	<-exit
	err = c.Close()
	if err != nil {
		panic(err)
	}
}

type ClientHandler struct{}

func (ClientHandler) HandlePub(incomingMessage frisbee.Message, incomingContent []byte) (outgoingMessage *frisbee.Message, outgoingContent []byte, action frisbee.Action) {
	panic("implement me")
}

func (ClientHandler) HandleSub(incomingMessage frisbee.Message, incomingContent []byte) (outgoingMessage *frisbee.Message, outgoingContent []byte, action frisbee.Action) {
	panic("implement me")
}
