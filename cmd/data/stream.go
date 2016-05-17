package data

import "github.com/WatchBeam/rtmp/chunk"

// Type Stream encapsulates a continuous stream of data messages coming over
// an RTMP chunk stream. The Stream parses each full chunk that it receives and
// emits it as a Data type over the In() chan. If an error was encountered
// during parsing, then that is retuend over the Errs() chan instead.
type Stream struct {
	// chunks represents the chunk stream that the data chunks are being
	// received over.
	chunks chan *chunk.Chunk
	// parser is the *Parser that is used to parse chunks from the
	// `*chunk.Stream` into `Data`s.
	parser Parser

	// writer is the chunk.Writer that is used to write data back to the
	// client in the RTMP chunk format.
	writer chunk.Writer

	// in holds each parsed Data token until it can be read somewhere else.
	in chan Data
	// errs holds all of the errors that were encountered during parsing.
	errs chan error
	// closer is written to when the Stream is told to close itself. When a
	// message is read over this channel, the Stream is expected to clean up
	// after itself.
	closer chan struct{}
}

// NewStream creates and returns a pointer to a new instance of the Stream type.
// The instance is initialized with the given chunk stream, and all of the
// internal channels are `make()`-d.
func NewStream(chunks chan *chunk.Chunk, writer chunk.Writer) *Stream {
	return &Stream{
		chunks: chunks,
		writer: writer,
		parser: DefaultParser,

		in:     make(chan Data),
		errs:   make(chan error),
		closer: make(chan struct{}),
	}
}

func (s *Stream) Chunks() chan<- *chunk.Chunk { return s.chunks }

// In returns a channel which is written to when a full Data payload can be
// parsed from the RTMP chunk stream on which this `*data.Stream` is listening.
func (s *Stream) In() <-chan Data { return s.in }

// Errs returns a channel of errors which is written to when an error is
// encountered during parsing.
func (s *Stream) Errs() <-chan error { return s.errs }

// Close closes the `*data.Stream`, causing it to stop listening as well as
// close all internal channels.
func (s *Stream) Close() { s.closer <- struct{}{} }

// Write writes the given frame of data "f" our to the chunk stream. If any
// error occured during marshaling or writing, then it will be returned, and the
// frame may not have been written correctly, indicating that the connection
// should be terminated.
//
// Successfully, a value of "nil" will be returned and the chunk can be assumed
// to have been successfully written.
func (s *Stream) Write(f Data) error {
	c, err := f.Marshal()
	if err != nil {
		return err
	}

	if err = s.writer.Write(c); err != nil {
		return err
	}

	return nil
}

// SetParser sets the intenral parser used by this Stream. This method is _not_
// safe to use between multiple goroutines, and should be used with caution.
func (s *Stream) SetParser(p Parser) { s.parser = p }

// Recv processes all incoming chunks off of the owned `*chunk.Stream` and
// parses them into Data types. If that parsing was succesful, the resulting
// Data type is passed to the appropriate channel. Otherwise, an error is pushed
// onto the `errs` channel.
//
// Recv also reads from the `out` channel when data is available on it, marshals
// it using the Data.Marshal function, and then sends it over the chunk stream.
//
// Recv also wathces the internal closer channel so that this `*data.Stream` may
// clean up after itself post-closing.
//
// Recv runs within its own goroutine.
func (s *Stream) Recv() {
	defer func() {
		close(s.in)
		close(s.errs)
		close(s.closer)
	}()

	for {
		select {
		case chunk := <-s.chunks:
			data, err := s.parser.Parse(chunk)
			if err != nil {
				s.errs <- err
				continue
			}

			s.in <- data
		case <-s.closer:
			return
		}
	}
}
