package control

import "github.com/WatchBeam/rtmp/chunk"

// Stream represents an RTMP-compliant bi-directional transfer of RTMP control
// sequences. It parses control sequences out of a chunk.Stream, and writes them
// back when they are sent into the stream.
type Stream struct {
	chunks chunk.Stream
	writer chunk.Writer

	in     chan Control
	errs   chan error
	closer chan struct{}

	parser  Parser
	chunker Chunker
}

// NewStream returns a new instance of the Stream type initialized with the
// given chunk stream, parser, and chunker.
func NewStream(chunks chunk.Stream, writer chunk.Writer,
	parser Parser, chunker Chunker) *Stream {

	return &Stream{
		chunks: chunks,
		writer: writer,

		in:     make(chan Control),
		errs:   make(chan error),
		closer: make(chan struct{}),

		parser:  parser,
		chunker: chunker,
	}
}

// In is written to when incoming control sequences are read off of the stream
func (s *Stream) In() <-chan Control { return s.in }

// Errs is written to when an error is encountered from the chunk stream, or an
// error is encountered in chunking or parsing.
func (s *Stream) Errs() <-chan error { return s.errs }

// Close stops the Recv goroutine.
func (s *Stream) Close() { s.closer <- struct{}{} }

// Send sends the given control "c", returning any errors that it encountered
// along the way.
func (s *Stream) Send(c Control) error {
	chunk, err := s.chunker.Chunk(c)
	if err != nil {
		return err
	}

	if err = s.writer.Write(chunk); err != nil {
		return err
	}

	return nil
}

// Recv processes input from all channels, as well as the incoming chunk
// streams.
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
		case <-s.closer:
			return
		case c := <-s.chunks.In():
			control, err := s.parser.Parse(c)
			if err != nil {
				s.errs <- err
				continue
			}

			s.in <- control
		}
	}
}
