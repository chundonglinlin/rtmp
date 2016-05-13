package server_test

import (
	"net"
	"testing"
	"time"

	"github.com/WatchBeam/rtmp/client"
	"github.com/WatchBeam/rtmp/server"
	"github.com/stretchr/testify/assert"
)

func TestNewServerConstructsServerWithValidBind(t *testing.T) {
	s, err := server.New("127.0.0.1:1234")
	defer s.Close()

	assert.IsType(t, &server.Server{}, s)
	assert.Nil(t, err)
}

func TestServerSocketReturnsNew(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:1935")
	assert.Nil(t, err)

	s := server.NewSocket(l.(*net.TCPListener))
	defer s.Close()

	assert.IsType(t, &server.Server{}, s)
}

func TestServerFailsWithInvalidBind(t *testing.T) {
	s, err := server.New("256.256.256.256:1234")

	assert.IsType(t, &server.Server{}, s)
	assert.NotNil(t, err)
}

func TestListenGetsNewClients(t *testing.T) {
	s, err := server.New("127.0.0.1:1935")
	assert.Nil(t, err)

	go s.Accept()
	defer s.Close()

	_, err = net.Dial("tcp", "127.0.0.1:1935")
	assert.Nil(t, err)

	assert.IsType(t, &client.Client{}, <-s.Clients())
}

func TestReleasesConnection(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:1935")
	assert.Nil(t, err)

	s := server.NewSocket(l.(*net.TCPListener))
	defer s.Close()

	acceptReturn := make(chan struct{})
	go func() {
		s.Accept()
		close(acceptReturn)
	}()

	assert.Equal(t, l, s.Release())

	select {
	case <-acceptReturn:
	case <-time.After(100 * time.Millisecond):
		assert.Fail(t, "timeout: xpected to Accept() to have returned when release is called")
	}
}
