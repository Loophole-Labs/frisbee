package frisbee

import (
	"bufio"
	"github.com/loophole-labs/frisbee/internal/protocol"
	"github.com/loophole-labs/frisbee/internal/ringbuffer"
	"github.com/pkg/errors"
	"net"
	"sync"
	"time"
)

const (
	defaultSize = 1 << 18
)

type Conn struct {
	sync.Mutex
	conn     net.Conn
	writer   *bufio.Writer
	flush    *sync.Cond
	reader   *bufio.Reader
	context  interface{}
	messages *ringbuffer.RingBuffer
	closed   bool
}

func Connect(network string, addr string, keepAlive time.Duration) (*Conn, error) {
	conn, err := net.Dial(network, addr)
	_ = conn.(*net.TCPConn).SetKeepAlive(true)
	_ = conn.(*net.TCPConn).SetKeepAlivePeriod(keepAlive)
	_ = conn.(*net.TCPConn).SetReadBuffer(defaultSize)
	_ = conn.(*net.TCPConn).SetWriteBuffer(defaultSize)
	if err != nil {
		return nil, err
	}
	return New(conn), nil
}

func New(c net.Conn) (conn *Conn) {
	conn = &Conn{
		conn:     c,
		writer:   bufio.NewWriterSize(c, defaultSize),
		reader:   bufio.NewReaderSize(c, defaultSize),
		messages: ringbuffer.NewRingBuffer(defaultSize),
		closed:   false,
		context:  nil,
	}

	conn.flush = sync.NewCond(conn)

	go conn.flushLoop()
	go conn.readLoop()

	return
}

func (c *Conn) Raw() (err error, conn net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	c.Lock()
	defer c.Unlock()
	c.messages.Close()
	c.closed = true
	c.flush.Broadcast()
	return nil, c.conn
}

func (c *Conn) Context() interface{} {
	return c.context
}

func (c *Conn) SetContext(ctx interface{}) {
	c.context = ctx
}

func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Conn) flushLoop() {
	for {
		c.flush.L.Lock()
		for c.writer.Buffered() == 0 {
			c.flush.Wait()
		}
		if c.closed {
			c.flush.L.Unlock()
			return
		}
		_ = c.writer.Flush()
		c.flush.L.Unlock()
	}
}

func (c *Conn) Write(message *Message, content *[]byte) error {
	if content != nil && int(message.ContentLength) != len(*content) {
		return errors.New("invalid content length")
	}

	if c.closed {
		return errors.New("connection closed")
	}
	encodedMessage, _ := protocol.EncodeV0(message.Id, message.Operation, message.Routing, message.ContentLength)
	c.Lock()
	_, _ = c.writer.Write(encodedMessage[:])
	if content != nil {
		_, _ = c.writer.Write(*content)
	}
	c.Unlock()

	c.flush.Signal()

	return nil
}

func (c *Conn) readLoop() {
	for {
		message, err := c.reader.Peek(protocol.HeaderLengthV0)
		if len(message) == protocol.HeaderLengthV0 {
			decodedMessage, err := protocol.DecodeV0(message)
			if err != nil {
				_, _ = c.reader.Discard(c.reader.Buffered())
				continue
			}
			if decodedMessage.ContentLength > 0 {
				if content, _ := c.reader.Peek(int(decodedMessage.ContentLength + protocol.HeaderLengthV0)); len(content) == int(decodedMessage.ContentLength+protocol.HeaderLengthV0) {
					readContent := make([]byte, decodedMessage.ContentLength)
					copy(readContent, content[protocol.HeaderLengthV0:])
					_, _ = c.reader.Discard(int(decodedMessage.ContentLength + protocol.HeaderLengthV0))
					readPacket := &protocol.PacketV0{
						Message: &decodedMessage,
						Content: &readContent,
					}
					_ = c.messages.Push(readPacket)
				}
				continue
			}
			_, _ = c.reader.Discard(protocol.HeaderLengthV0)
			readPacket := &protocol.PacketV0{
				Message: &decodedMessage,
				Content: nil,
			}
			_ = c.messages.Push(readPacket)
		}
		if err != nil {
			_ = c.Close()
			return
		}
	}
}

func (c *Conn) Read() (*Message, *[]byte, error) {
	if c.closed {
		return nil, nil, errors.New("connection closed")
	}

	readPacket, err := c.messages.Pop()
	if err != nil {
		return nil, nil, errors.New("unable to retrieve packet")
	}

	return (*Message)(readPacket.Message), readPacket.Content, nil
}

func (c *Conn) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	c.Lock()
	defer c.Unlock()
	c.messages.Close()
	c.closed = true
	c.flush.Broadcast()
	return c.conn.Close()
}
