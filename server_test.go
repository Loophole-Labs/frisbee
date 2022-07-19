/*
	Copyright 2022 Loophole Labs

	Licensed under the Apache License, Version 2.0 (the "License");
	you may not use this file except in compliance with the License.
	You may obtain a copy of the License at

		   http://www.apache.org/licenses/LICENSE-2.0

	Unless required by applicable law or agreed to in writing, software
	distributed under the License is distributed on an "AS IS" BASIS,
	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
	See the License for the specific language governing permissions and
	limitations under the License.
*/

package frisbee

import (
	"context"
	"crypto/rand"
	"github.com/loopholelabs/frisbee/pkg/metadata"
	"github.com/loopholelabs/frisbee/pkg/packet"
	"github.com/loopholelabs/testing/conn/pair"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net"
	"runtime"
	"sync"
	"testing"
)

// trunk-ignore-all(golangci-lint/staticcheck)

const (
	serverConnContextKey = "conn"
)

func TestServerRaw(t *testing.T) {
	t.Parallel()

	const testSize = 100
	const packetSize = 512
	clientHandlerTable := make(HandlerTable)
	serverHandlerTable := make(HandlerTable)

	serverIsRaw := make(chan struct{}, 1)

	serverHandlerTable[metadata.PacketPing] = func(_ context.Context, _ *packet.Packet) (outgoing *packet.Packet, action Action) {
		return
	}

	var rawServerConn, rawClientConn net.Conn
	serverHandlerTable[metadata.PacketProbe] = func(ctx context.Context, _ *packet.Packet) (outgoing *packet.Packet, action Action) {
		conn := ctx.Value(serverConnContextKey).(*Async)
		rawServerConn = conn.Raw()
		serverIsRaw <- struct{}{}
		return
	}

	clientHandlerTable[metadata.PacketPing] = func(_ context.Context, _ *packet.Packet) (outgoing *packet.Packet, action Action) {
		return
	}

	emptyLogger := zerolog.New(ioutil.Discard)
	s, err := NewServer(serverHandlerTable, WithLogger(&emptyLogger))
	require.NoError(t, err)

	s.ConnContext = func(ctx context.Context, c *Async) context.Context {
		return context.WithValue(ctx, serverConnContextKey, c)
	}

	serverConn, clientConn, err := pair.New()
	require.NoError(t, err)

	go s.ServeConn(serverConn)

	c, err := NewClient(clientHandlerTable, context.Background(), WithLogger(&emptyLogger))
	assert.NoError(t, err)

	_, err = c.Raw()
	assert.ErrorIs(t, ConnectionNotInitialized, err)

	err = c.FromConn(clientConn)
	assert.NoError(t, err)

	data := make([]byte, packetSize)
	_, _ = rand.Read(data)
	p := packet.Get()
	p.Content.Write(data)
	p.Metadata.ContentLength = packetSize
	p.Metadata.Operation = metadata.PacketPing
	assert.EqualValues(t, data, *p.Content)

	for q := 0; q < testSize; q++ {
		p.Metadata.Id = uint16(q)
		err = c.WritePacket(p)
		assert.NoError(t, err)
	}

	p.Reset()
	assert.Equal(t, 0, len(*p.Content))
	p.Metadata.Operation = metadata.PacketProbe

	err = c.WritePacket(p)
	require.NoError(t, err)

	packet.Put(p)

	rawClientConn, err = c.Raw()
	require.NoError(t, err)

	<-serverIsRaw

	serverBytes := []byte("SERVER WRITE")

	write, err := rawServerConn.Write(serverBytes)
	assert.NoError(t, err)
	assert.Equal(t, len(serverBytes), write)

	clientBuffer := make([]byte, len(serverBytes))
	read, err := rawClientConn.Read(clientBuffer)
	assert.NoError(t, err)
	assert.Equal(t, len(serverBytes), read)

	assert.Equal(t, serverBytes, clientBuffer)

	err = c.Close()
	assert.NoError(t, err)
	err = rawClientConn.Close()
	assert.NoError(t, err)

	err = s.Shutdown()
	assert.NoError(t, err)
	err = rawServerConn.Close()
	assert.NoError(t, err)
}

func TestServerStaleClose(t *testing.T) {
	t.Parallel()

	const testSize = 100
	const packetSize = 512
	clientHandlerTable := make(HandlerTable)
	serverHandlerTable := make(HandlerTable)

	finished := make(chan struct{}, 1)

	serverHandlerTable[metadata.PacketPing] = func(_ context.Context, incoming *packet.Packet) (outgoing *packet.Packet, action Action) {
		if incoming.Metadata.Id == testSize-1 {
			outgoing = incoming
			action = CLOSE
		}
		return
	}

	clientHandlerTable[metadata.PacketPing] = func(_ context.Context, _ *packet.Packet) (outgoing *packet.Packet, action Action) {
		finished <- struct{}{}
		return
	}

	emptyLogger := zerolog.New(ioutil.Discard)
	s, err := NewServer(serverHandlerTable, WithLogger(&emptyLogger))
	require.NoError(t, err)

	serverConn, clientConn, err := pair.New()
	require.NoError(t, err)

	go s.ServeConn(serverConn)

	c, err := NewClient(clientHandlerTable, context.Background(), WithLogger(&emptyLogger))
	assert.NoError(t, err)
	_, err = c.Raw()
	assert.ErrorIs(t, ConnectionNotInitialized, err)

	err = c.FromConn(clientConn)
	require.NoError(t, err)

	data := make([]byte, packetSize)
	_, _ = rand.Read(data)
	p := packet.Get()
	p.Content.Write(data)
	p.Metadata.ContentLength = packetSize
	p.Metadata.Operation = metadata.PacketPing
	assert.EqualValues(t, data, *p.Content)

	for q := 0; q < testSize; q++ {
		p.Metadata.Id = uint16(q)
		err = c.WritePacket(p)
		assert.NoError(t, err)
	}
	packet.Put(p)
	<-finished

	_, err = c.conn.ReadPacket()
	assert.ErrorIs(t, err, ConnectionClosed)

	err = c.Close()
	assert.NoError(t, err)

	err = s.Shutdown()
	assert.NoError(t, err)
}

func BenchmarkThroughputServer(b *testing.B) {
	const testSize = 1<<16 - 1
	const packetSize = 512

	handlerTable := make(HandlerTable)

	handlerTable[metadata.PacketPing] = func(_ context.Context, _ *packet.Packet) (outgoing *packet.Packet, action Action) {
		return
	}

	emptyLogger := zerolog.New(ioutil.Discard)
	server, err := NewServer(handlerTable, WithLogger(&emptyLogger))
	if err != nil {
		b.Fatal(err)
	}

	serverConn, clientConn, err := pair.New()
	if err != nil {
		b.Fatal(err)
	}

	go server.ServeConn(serverConn)

	frisbeeConn := NewAsync(clientConn, &emptyLogger)

	data := make([]byte, packetSize)
	_, _ = rand.Read(data)
	p := packet.Get()
	p.Metadata.Operation = metadata.PacketPing

	p.Content.Write(data)
	p.Metadata.ContentLength = packetSize

	b.Run("test", func(b *testing.B) {
		b.SetBytes(testSize * packetSize)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for q := 0; q < testSize; q++ {
				p.Metadata.Id = uint16(q)
				err = frisbeeConn.WritePacket(p)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.StopTimer()

	packet.Put(p)

	err = frisbeeConn.Close()
	if err != nil {
		b.Fatal(err)
	}
	err = server.Shutdown()
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkThroughputResponseServer(b *testing.B) {
	const testSize = 1<<16 - 1
	const packetSize = 512

	serverConn, clientConn, err := pair.New()
	if err != nil {
		b.Fatal(err)
	}

	handlerTable := make(HandlerTable)

	handlerTable[metadata.PacketPing] = func(_ context.Context, incoming *packet.Packet) (outgoing *packet.Packet, action Action) {
		if incoming.Metadata.Id == testSize-1 {
			incoming.Reset()
			incoming.Metadata.Id = testSize
			incoming.Metadata.Operation = metadata.PacketPong
			outgoing = incoming
		}
		return
	}

	emptyLogger := zerolog.New(ioutil.Discard)
	server, err := NewServer(handlerTable, WithLogger(&emptyLogger))
	if err != nil {
		b.Fatal(err)
	}

	go server.ServeConn(serverConn)

	frisbeeConn := NewAsync(clientConn, &emptyLogger)

	data := make([]byte, packetSize)
	_, _ = rand.Read(data)

	p := packet.Get()
	p.Metadata.Operation = metadata.PacketPing

	p.Content.Write(data)
	p.Metadata.ContentLength = packetSize

	b.Run("test", func(b *testing.B) {
		b.SetBytes(testSize * packetSize)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for q := 0; q < testSize; q++ {
				p.Metadata.Id = uint16(q)
				err = frisbeeConn.WritePacket(p)
				if err != nil {
					b.Fatal(err)
				}
			}
			readPacket, err := frisbeeConn.ReadPacket()
			if err != nil {
				b.Fatal(err)
			}

			if readPacket.Metadata.Id != testSize {
				b.Fatal("invalid decoded metadata id", readPacket.Metadata.Id)
			}

			if readPacket.Metadata.Operation != metadata.PacketPong {
				b.Fatal("invalid decoded operation", readPacket.Metadata.Operation)
			}
			packet.Put(readPacket)
		}

	})
	b.StopTimer()

	packet.Put(p)

	err = frisbeeConn.Close()
	if err != nil {
		b.Fatal(err)
	}
	err = server.Shutdown()
	if err != nil {
		b.Fatal(err)
	}
}

func BenchmarkAsyncThroughputNetworkMultiple(b *testing.B) {
	const testSize = 100

	throughputRunner := func(testSize uint32, packetSize uint32, readerConn Conn, writerConn Conn) func(b *testing.B) {
		return func(b *testing.B) {
			var err error

			randomData := make([]byte, packetSize)

			p := packet.Get()
			p.Metadata.Id = 64
			p.Metadata.Operation = 32
			p.Content.Write(randomData)
			p.Metadata.ContentLength = packetSize
			for i := 0; i < b.N; i++ {
				done := make(chan struct{}, 1)
				errCh := make(chan error, 1)
				go func() {
					for i := uint32(0); i < testSize; i++ {
						p, err := readerConn.ReadPacket()
						if err != nil {
							errCh <- err
							return
						}
						packet.Put(p)
					}
					done <- struct{}{}
				}()
				for i := uint32(0); i < testSize; i++ {
					select {
					case err = <-errCh:
						b.Fatal(err)
					default:
						err = writerConn.WritePacket(p)
						if err != nil {
							b.Fatal(err)
						}
					}
				}
				select {
				case <-done:
					continue
				case err = <-errCh:
					b.Fatal(err)
				}
			}

			packet.Put(p)
		}
	}

	runner := func(numClients int, packetSize uint32) func(b *testing.B) {
		return func(b *testing.B) {
			var wg sync.WaitGroup
			wg.Add(numClients)
			b.SetBytes(int64(testSize * packetSize))
			b.ReportAllocs()
			for i := 0; i < numClients; i++ {
				go func() {
					emptyLogger := zerolog.New(ioutil.Discard)

					reader, writer, err := pair.New()
					if err != nil {
						b.Error(err)
					}

					readerConn := NewAsync(reader, &emptyLogger)
					writerConn := NewAsync(writer, &emptyLogger)
					throughputRunner(testSize, packetSize, readerConn, writerConn)(b)

					_ = readerConn.Close()
					_ = writerConn.Close()
					wg.Done()
				}()
			}
			wg.Wait()
		}
	}

	b.Run("1 Pair, 32 Bytes", runner(1, 32))
	b.Run("2 Pair, 32 Bytes", runner(2, 32))
	b.Run("5 Pair, 32 Bytes", runner(5, 32))
	b.Run("10 Pair, 32 Bytes", runner(10, 32))
	b.Run("Half CPU Pair, 32 Bytes", runner(runtime.NumCPU()/2, 32))
	b.Run("CPU Pair, 32 Bytes", runner(runtime.NumCPU(), 32))

	b.Run("1 Pair, 512 Bytes", runner(1, 512))
	b.Run("2 Pair, 512 Bytes", runner(2, 512))
	b.Run("5 Pair, 512 Bytes", runner(5, 512))
	b.Run("10 Pair, 512 Bytes", runner(10, 512))
	b.Run("Half CPU Pair, 512 Bytes", runner(runtime.NumCPU()/2, 512))
	b.Run("CPU Pair, 512 Bytes", runner(runtime.NumCPU(), 512))

	b.Run("1 Pair, 4096 Bytes", runner(1, 4096))
	b.Run("2 Pair, 4096 Bytes", runner(2, 4096))
	b.Run("5 Pair, 4096 Bytes", runner(5, 4096))
	b.Run("10 Pair, 4096 Bytes", runner(10, 4096))
	b.Run("Half CPU Pair, 4096 Bytes", runner(runtime.NumCPU()/2, 4096))
	b.Run("CPU Pair, 4096 Bytes", runner(runtime.NumCPU(), 4096))
}
