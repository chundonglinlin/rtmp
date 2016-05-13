package server

import (
	"net"
	"time"

	"github.com/WatchBeam/rtmp/client"
)

// A Server represents a TCP server capable of accepting connections, and
// pushing them into the Clients() channel.
//
// Underneath the hood, type `Server` uses a net.Listener (of the TCP-variety)
// to listen for connections, and maintains a channel for errors, as well as a
// channel for clients.
type Server struct {
	// socket is the net.Listener which enables the `Server` type to listen
	// for TCP connections.
	socket *net.TCPListener

	// the deadline determines how long to wait in listeners before loop
	// again. This is mostly internal, and just specifies the maximum wait
	// time in the call to .Release()
	deadline time.Duration

	// clients is a non-buffered channel of *client.Client, which is
	// populated each time a client connects.
	clients chan *client.Client
	// errs is a channel of errors that is written to every time an error is
	// encountered in the Accept routine.
	errs chan error
	// release is a signaler indicating to the Accept() loop to exit and
	// stop accepting connections on the listener. The Accept() loop listens
	// to writes to this channel, and will close the channel when it exits.
	release chan struct{}
}

// NewBound instantiates and returns a new server, bound to the `bind` address
// given. Semantics for `bind` follow those set forth in the `net` package.
// Calling `New()` does in-fact create a TCP Listener on that address, and
// returns an error if the address is non-parsable, or the network is not
// able to be bound.
//
// Otherwise, a server is returned.
func New(bind string) (*Server, error) {
	network := "tcp"
	addr, err := net.ResolveTCPAddr(network, bind)
	if err != nil {
		return nil, err
	}

	socket, err := net.ListenTCP(network, addr)
	if err != nil {
		return nil, err
	}

	return NewSocket(socket), nil
}

// New instantiates a new RTMP server which listens on
// the provided network socket.
func NewSocket(socket *net.TCPListener) *Server {
	return &Server{
		socket:   socket,
		deadline: time.Second,
		clients:  make(chan *client.Client),
		errs:     make(chan error),
		release:  make(chan struct{}),
	}
}

// Close closes the network socket, terminating the processof accepting new
// connections immediately..
func (s *Server) Close() error {
	return s.socket.Close()
}

// Clients returns a read-only channel of *client.Client, written to when a new
// connection is obtained into the server.
func (s *Server) Clients() <-chan *client.Client {
	return s.clients
}

// Errs returns a read-only channel of `error`s, written to when accepting
// a socket connection returns an error.
func (s *Server) Errs() <-chan error {
	return s.errs
}

// Release halts the Accept() loop and returns the net listener. When this
// method returns, existing clients will remain connected but the listener
// will no longer be in use.
func (s *Server) Release() *net.TCPListener {
	s.release <- struct{}{}
	<-s.release
	return s.socket
}

// Accept encapsulates the process of accepting new clients to the server.
//
// In the failing case, if an error is returned from attempting to connect to a
// socket, then the error will be piped up to the Errs() channel, and the
// connection request will be ignreod.
//
// In the successful case, the client is written to the internal `clients`
// channel, which is readable from the Clients() method.
//
// Accept runs within its own goroutine.
func (s *Server) Accept() {
	defer close(s.release)
	l := s.socket

	for {
		l.SetDeadline(time.Now().Add(s.deadline))
		conn, err := l.Accept()

		if err != nil {
			if nerr, ok := err.(net.Error); !ok || !nerr.Timeout() {
				s.errs <- err
			}
		} else {
			s.clients <- client.New(conn)
		}

		select {
		case <-s.release:
			return
		default:
		}
	}
}
