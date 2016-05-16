package server

import (
	"net"
	"sync"
	"time"

	"github.com/WatchBeam/rtmp/client"
)

const (
	// network to use in .New()
	defaultNetwork = "tcp"
)

// state is the internal state type used within the Server. Diagram:
//
// //       +---------+
// //       |  IDLE   |
// //       +---------+
// //            |
// //       .Accept()
// //            v
// //       +---------+
// //       |ACCEPTING|
// //       +---------+
// //            |
// //      +-----+-----+
// //  .Close()   .Release()
// //      v           v
// // +---------+ +---------+
// // | CLOSING | |RELEASING|
// // +---------+ +---------+
// //     |            |
// //     +------+-----+
// //            v
// //       +---------+
// //       | CLOSED  |
// //       +---------+
type state uint8

const (
	idle state = iota
	accepting
	closing
	releasing
	closed
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

	// record if the internal state of the server:
	scond *sync.Cond
	state state
}

// NewBound instantiates and returns a new server, bound to the `bind` address
// given. Semantics for `bind` follow those set forth in the `net` package.
// Calling `New()` does in-fact create a TCP Listener on that address, and
// returns an error if the address is non-parsable, or the network is not
// able to be bound.
//
// Otherwise, a server is returned.
func New(bind string) (*Server, error) {
	addr, err := net.ResolveTCPAddr(defaultNetwork, bind)
	if err != nil {
		return nil, err
	}

	socket, err := net.ListenTCP(defaultNetwork, addr)
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
		scond:    sync.NewCond(&sync.Mutex{}),
	}
}

// Close closes the network socket, terminating the processof accepting new
// connections immediately..
func (s *Server) Close() error {
	s.scond.L.Lock()
	defer s.scond.L.Unlock()
	if err := s.socket.Close(); err != nil {
		return err
	}

	if s.state != accepting {
		s.state = closed
		return nil
	}

	s.state = closing
	s.waitForState(closed)
	return nil
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
	s.scond.L.Lock()
	defer s.scond.L.Unlock()

	if s.state != accepting {
		s.state = closed
		return s.socket
	}

	s.state = releasing
	s.socket.SetDeadline(time.Now())
	s.waitForState(closed)

	return s.socket
}

// handleError examines the provided error object and returns true if
// the accept loop should be terminated.
func (s *Server) handleError(err error) (kill bool) {
	s.scond.L.Lock()
	defer s.scond.L.Unlock()

	nerr, ok := err.(net.Error)
	// non-network errors can just be sent straight down
	if !ok {
		s.errs <- err
	}

	// Time outs are used to signal when we want to release the socket.
	if s.state == releasing && nerr.Timeout() {
		return true
	}

	// If we're closing and it's a close error, that's perfect, just return!
	if s.state == closing && isNetCloseError(nerr) {
		return true
	}

	// If it's some other kind of temporary error, log it and continue.
	if nerr.Temporary() {
		s.errs <- err
		return false
	}

	// Otherwise it's a non-temporary network error. Send it
	// down the error channel and kill the accept loop.
	s.errs <- fatalError{err}
	return true
}

// setState transitions to the target state and emits a broadcast to listeners
// on the condition.
func (s *Server) setState(v state) {
	s.scond.L.Lock()
	defer s.scond.L.Unlock()
	s.state = v
	s.scond.Broadcast()
}

// setStateIf modifies the current state if the predicate returns true.
func (s *Server) setStateIf(to state, predicate func(v state) bool) (changed bool) {
	s.scond.L.Lock()
	defer s.scond.L.Unlock()
	if predicate(s.state) {
		s.state = to
		s.scond.Broadcast()
		return true
	}

	return false
}

// waitForState blocks until the server enters the `expected` state. It's
// expected the caller initially has a lock when invoking this function,
// and this function will maintain that lock when it returns.
func (s *Server) waitForState(expected state) {
	for {
		if expected == s.state {
			return
		}

		s.scond.Wait()
	}
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
	// As soon as we start the accept loop, make sure we're in the idle state.
	// If not someone probably already closed or released us before this
	// routine was scheduled!
	if ok := s.setStateIf(accepting, func(v state) bool { return v == idle }); !ok {
		return
	}
	defer s.setState(closed)

	for {
		conn, err := s.socket.Accept()

		if err == nil {
			s.clients <- client.New(conn)
		} else if kill := s.handleError(err); kill {
			return
		}
	}
}
