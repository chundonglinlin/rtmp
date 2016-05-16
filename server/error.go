package server

import (
	"net"
	"strings"
)

// A FatalError is send down the Server.Errs() channel if an *unexpected*
// event occurred which caused the server to stop accepting connections.
// A FatalError will not be sent when Accept() terminates as a result of a
// call to .Close() or .Release().
type FatalError interface {
	error
	IsFatal() bool
}

type fatalError struct{ error }

var _ FatalError = fatalError{}

func (f fatalError) IsFatal() bool {
	return true
}

// isNetCloseError returns true the `err` is a result of a closed network
// connection. This is a really horrible way to do it, however, there is
// no obvious better solution. The error is not exported nor wrapped helpfully:
// https://github.com/golang/go/blob/b6b4004d5a5bf7099ac9ab76777797236da7fe63/src/net/net.go#L377
func isNetCloseError(err net.Error) bool {
	return strings.Contains(err.Error(), "use of closed network connection")
}
