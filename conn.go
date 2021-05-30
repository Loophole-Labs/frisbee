package frisbee

import (
	"bufio"
	"encoding/binary"
	"github.com/gobwas/pool/pbufio"
	"github.com/loophole-labs/frisbee/internal/errors"
	"github.com/loophole-labs/frisbee/internal/protocol"
	"github.com/loophole-labs/frisbee/internal/ringbuffer"
	"github.com/rs/zerolog"
	"go.uber.org/atomic"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

const (
	CONNECTED = int32(iota)
	CLOSED
	PAUSED
)

var (
	writePool     = pbufio.NewWriterPool(1024, 1<<32)
	defaultLogger = zerolog.New(os.Stdout)
)

type Conn struct {
	sync.Mutex
	conn             net.Conn
	state            *atomic.Int32
	writer           *bufio.Writer
	flusher          chan struct{}
	incomingMessages *ringbuffer.RingBuffer
	logger           *zerolog.Logger
	wg               sync.WaitGroup
	error            *atomic.Error
}

func Connect(network string, addr string, keepAlive time.Duration, l *zerolog.Logger) (*Conn, error) {
	conn, err := net.Dial(network, addr)
	if err != nil {
		return nil, errors.WithContext(err, DIAL)
	}
	_ = conn.(*net.TCPConn).SetKeepAlive(true)
	_ = conn.(*net.TCPConn).SetKeepAlivePeriod(keepAlive)

	return New(conn, l), nil
}

func New(c net.Conn, l *zerolog.Logger) (conn *Conn) {
	conn = &Conn{
		conn:             c,
		state:            atomic.NewInt32(CONNECTED),
		writer:           writePool.Get(c, 1<<19),
		incomingMessages: ringbuffer.NewRingBuffer(1 << 19),
		flusher:          make(chan struct{}, 1024),
		logger:           l,
		error:            atomic.NewError(ConnectionClosed),
	}

	if l == nil {
		conn.logger = &defaultLogger
	}

	conn.wg.Add(2)
	go conn.flushLoop()
	go conn.readLoop()

	return
}

func (c *Conn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *Conn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Conn) Write(message *Message, content *[]byte) error {
	if content != nil && int(message.ContentLength) != len(*content) {
		return InvalidContentLength
	}

	var encodedMessage [protocol.MessageV0Size]byte

	binary.BigEndian.PutUint16(encodedMessage[protocol.VersionV0Offset:protocol.VersionV0Offset+protocol.VersionV0Size], protocol.Version0)
	binary.BigEndian.PutUint32(encodedMessage[protocol.FromV0Offset:protocol.FromV0Offset+protocol.FromV0Size], message.From)
	binary.BigEndian.PutUint32(encodedMessage[protocol.ToV0Offset:protocol.ToV0Offset+protocol.ToV0Size], message.To)
	binary.BigEndian.PutUint32(encodedMessage[protocol.IdV0Offset:protocol.IdV0Offset+protocol.IdV0Size], message.Id)
	binary.BigEndian.PutUint32(encodedMessage[protocol.OperationV0Offset:protocol.OperationV0Offset+protocol.OperationV0Size], message.Operation)
	binary.BigEndian.PutUint64(encodedMessage[protocol.ContentLengthV0Offset:protocol.ContentLengthV0Offset+protocol.ContentLengthV0Size], message.ContentLength)

	if c.state.Load() != CONNECTED {
		return c.Error()
	}

	c.Lock()
	_, err := c.writer.Write(encodedMessage[:])
	if err != nil {
		c.Unlock()
		if c.state.Load() != CONNECTED {
			err = c.Error()
			c.logger.Error().Msgf(errors.WithContext(err, WRITE).Error())
			return errors.WithContext(err, WRITE)
		}
		c.logger.Error().Msgf(errors.WithContext(err, WRITE).Error())
		return c.closeWithError(err)
	}
	if content != nil {
		_, err = c.writer.Write(*content)
		if err != nil {
			c.Unlock()
			if c.state.Load() != CONNECTED {
				err = c.Error()
				c.logger.Error().Msgf(errors.WithContext(err, WRITE).Error())
				return errors.WithContext(err, WRITE)
			}
			c.logger.Error().Msgf(errors.WithContext(err, WRITE).Error())
			return c.closeWithError(err)
		}
	}

	if len(c.flusher) == 0 {
		select {
		case c.flusher <- struct{}{}:
		default:
		}
	}

	c.Unlock()

	return nil
}

func (c *Conn) Flush() error {
	c.Lock()
	if c.writer.Buffered() > 0 {
		err := c.writer.Flush()
		if err != nil {
			c.Unlock()
			_ = c.closeWithError(err)
			return err
		}
	}
	c.Unlock()
	return nil
}

func (c *Conn) Read() (*Message, *[]byte, error) {
	if c.state.Load() != CONNECTED {
		return nil, nil, c.Error()
	}

	readPacket, err := c.incomingMessages.Pop()
	if err != nil {
		if c.state.Load() != CONNECTED {
			err = c.Error()
			c.logger.Error().Msgf(errors.WithContext(err, POP).Error())
			return nil, nil, errors.WithContext(err, POP)
		}
		c.logger.Error().Msgf(errors.WithContext(err, POP).Error())
		return nil, nil, errors.WithContext(c.closeWithError(err), POP)
	}

	return (*Message)(readPacket.Message), readPacket.Content, nil
}

func (c *Conn) Logger() *zerolog.Logger {
	return c.logger
}

func (c *Conn) Error() error {
	return c.error.Load()
}

func (c *Conn) Raw() net.Conn {
	_ = c.close()
	return c.conn
}

func (c *Conn) Close() error {
	err := c.close()
	if errors.Is(err, ConnectionClosed) {
		return nil
	}
	_ = c.conn.Close()
	return err
}

func (c *Conn) killGoroutines() {
	c.incomingMessages.Close()
	close(c.flusher)
	_ = c.conn.SetReadDeadline(time.Now())
	c.wg.Wait()
	_ = c.conn.SetReadDeadline(time.Time{})
}

func (c *Conn) pause() error {
	if c.state.CAS(CONNECTED, PAUSED) {
		c.error.Store(ConnectionPaused)
		c.killGoroutines()
		return nil
	} else if c.state.Load() == PAUSED {
		return ConnectionPaused
	}
	return ConnectionNotInitialized
}

func (c *Conn) close() error {
	if c.state.CAS(CONNECTED, CLOSED) {
		c.error.Store(ConnectionClosed)
		c.killGoroutines()
		c.Lock()
		if c.writer.Buffered() > 0 {
			_ = c.writer.Flush()
		}
		writePool.Put(c.writer)
		c.Unlock()
		return nil
	} else if c.state.CAS(PAUSED, CLOSED) {
		c.error.Store(ConnectionClosed)
		c.Lock()
		writePool.Put(c.writer)
		c.Unlock()
		return nil
	}
	return ConnectionClosed
}

func (c *Conn) closeWithError(err error) error {
	if os.IsTimeout(err) {
		c.Logger().Debug().Msgf("connection timed out")
		return err
	} else if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		pauseError := c.pause()
		if errors.Is(pauseError, ConnectionNotInitialized) {
			c.Logger().Debug().Msgf("attempted to close connection with error, but connection not initialized (inner error: %+v)", err)
			return ConnectionNotInitialized
		} else {
			c.Logger().Debug().Msgf("attempted to close connection with error, but error was EOF so pausing connection instead (inner error: %+v)", err)
			return ConnectionPaused
		}
	} else {
		closeError := c.close()
		if errors.Is(closeError, ConnectionClosed) {
			c.Logger().Debug().Msgf("attempted to close connection with error, but connection already closed (inner error: %+v)", err)
			return ConnectionClosed
		} else {
			c.Logger().Debug().Msgf("closing connection with error: %+v", err)
		}
	}
	c.error.Store(err)
	_ = c.conn.Close()
	return err
}

func (c *Conn) flushLoop() {
	for {
		if _, ok := <-c.flusher; !ok {
			c.wg.Done()
			return
		}
		c.Lock()
		if c.writer.Buffered() > 0 {
			err := c.writer.Flush()
			if err != nil {
				c.Unlock()
				c.wg.Done()
				_ = c.closeWithError(err)
				return
			}
		}
		c.Unlock()
	}
}

func (c *Conn) readLoop() {
	buf := make([]byte, 1<<19)
	var index int
	for {
		buf = buf[:cap(buf)]
		if len(buf) < protocol.MessageV0Size {
			c.wg.Done()
			_ = c.closeWithError(InvalidBufferLength)
			return
		}
		var n int
		var err error
		for n < protocol.MessageV0Size {
			var nn int
			nn, err = c.conn.Read(buf[n:])
			n += nn
			if err != nil {
				if n < protocol.MessageV0Size {
					c.wg.Done()
					_ = c.closeWithError(err)
					return
				}
				break
			}
		}

		index = 0
		for index < n {
			if binary.BigEndian.Uint16(buf[index+protocol.VersionV0Offset:index+protocol.VersionV0Offset+protocol.VersionV0Size]) != protocol.Version0 {
				c.Logger().Error().Msgf(InvalidBufferContents.Error())
				break
			}

			decodedMessage := protocol.MessageV0{
				From:          binary.BigEndian.Uint32(buf[index+protocol.FromV0Offset : index+protocol.FromV0Offset+protocol.FromV0Size]),
				To:            binary.BigEndian.Uint32(buf[index+protocol.ToV0Offset : index+protocol.ToV0Offset+protocol.ToV0Size]),
				Id:            binary.BigEndian.Uint32(buf[index+protocol.IdV0Offset : index+protocol.IdV0Offset+protocol.IdV0Size]),
				Operation:     binary.BigEndian.Uint32(buf[index+protocol.OperationV0Offset : index+protocol.OperationV0Offset+protocol.OperationV0Size]),
				ContentLength: binary.BigEndian.Uint64(buf[index+protocol.ContentLengthV0Offset : index+protocol.ContentLengthV0Offset+protocol.ContentLengthV0Size]),
			}
			index += protocol.MessageV0Size
			if decodedMessage.ContentLength > 0 {
				readContent := make([]byte, decodedMessage.ContentLength)
				if n-index < int(decodedMessage.ContentLength) {
					for cap(buf) < int(decodedMessage.ContentLength) {
						buf = append(buf[:cap(buf)], 0)
						buf = buf[:cap(buf)]
					}
					cp := copy(readContent, buf[index:n])
					buf = buf[:cap(buf)]
					min := int(decodedMessage.ContentLength) - cp
					if len(buf) < min {
						c.wg.Done()
						_ = c.closeWithError(InvalidBufferLength)
						return
					}
					n = 0
					for n < min {
						var nn int
						nn, err = c.conn.Read(buf[n:])
						n += nn
						if err != nil {
							if n < min {
								c.wg.Done()
								_ = c.closeWithError(err)

								return
							}
							break
						}
					}
					copy(readContent[cp:], buf[:min])
					index = min
				} else {
					copy(readContent, buf[index:index+int(decodedMessage.ContentLength)])
					index += int(decodedMessage.ContentLength)
				}
				err = c.incomingMessages.Push(&protocol.PacketV0{
					Message: &decodedMessage,
					Content: &readContent,
				})
				if err != nil {
					c.Logger().Debug().Msgf(errors.WithContext(err, PUSH).Error())
					c.wg.Done()
					_ = c.closeWithError(err)
					return
				}
			} else {
				err = c.incomingMessages.Push(&protocol.PacketV0{
					Message: &decodedMessage,
					Content: nil,
				})
				if err != nil {
					c.Logger().Debug().Msgf(errors.WithContext(err, PUSH).Error())
					c.wg.Done()
					_ = c.closeWithError(err)
					return
				}
			}
			if n == index {
				index = 0
				buf = buf[:cap(buf)]
				if len(buf) < protocol.MessageV0Size {
					c.wg.Done()
					_ = c.closeWithError(InvalidBufferLength)
					break
				}
				n = 0
				for n < protocol.MessageV0Size {
					var nn int
					nn, err = c.conn.Read(buf[n:])
					n += nn
					if err != nil {
						if n < protocol.MessageV0Size {
							c.wg.Done()
							_ = c.closeWithError(err)
							return
						}
						break
					}
				}
			} else if n-index < protocol.MessageV0Size {
				copy(buf, buf[index:n])
				n -= index
				index = n

				buf = buf[:cap(buf)]
				min := protocol.MessageV0Size - index
				if len(buf) < min {
					c.wg.Done()
					_ = c.closeWithError(InvalidBufferLength)
					break
				}
				n = 0
				for n < min {
					var nn int
					nn, err = c.conn.Read(buf[index+n:])
					n += nn
					if err != nil {
						if n < min {
							c.wg.Done()
							_ = c.closeWithError(err)
							return
						}
						break
					}
				}
				n += index
				index = 0
			}
		}
	}
}
