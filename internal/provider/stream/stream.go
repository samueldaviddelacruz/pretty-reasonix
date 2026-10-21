// Package stream drives a provider kind's event stream: it reads events from an HTTP response,
// hands each payload to the kind, and emits this program's chunks.
//
// Every kind needs exactly this loop — read, fold, emit, and end in one of the two ways the contract
// allows — and each one used to carry its own copy. The copies drifted: a bug in how a stream ended
// had to be found and fixed twice.
package stream

import (
	"context"
	"errors"
	"io"

	"pretty-reasonix/internal/contract/provider"
	"pretty-reasonix/internal/httpclient"
)

// Folder folds one event payload into chunks. It is what a kind supplies; the loop around it lives
// here.
//
// Fold reports that the vendor said the stream is over, and an error for a payload that could not be
// understood or a refusal the vendor sent inside a successful response. It must not emit on its own:
// everything it produces goes through emit, which reports whether the consumer is still there.
type Folder interface {
	Fold(payload string, emit func(provider.Chunk) bool) (over bool, err error)
}

// Pump runs the stream and returns its chunks. The channel is closed when the stream ends.
//
// The caller must drain the channel or cancel ctx. The goroutine that drives the loop blocks on the
// send, so abandoning it without doing one of those leaves it waiting for ever.
//
// A stream ends in exactly one of two ways, and the distinction matters to everything above it. A
// normal ending — the vendor's marker, a finished turn, or a plain close — emits ChunkDone, because
// the loop needs one unambiguous "nothing more is coming" rather than three vendor-specific ways to
// say it. A failure emits ChunkError and no ChunkDone at all, because a stream that failed did not
// finish, and reporting both would be reporting success after a failure.
func Pump(ctx context.Context, events *httpclient.Events, folder Folder) <-chan provider.Chunk {
	chunks := make(chan provider.Chunk)

	emit := func(chunk provider.Chunk) bool {
		select {
		case chunks <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}

	go func() {
		defer close(chunks)
		defer func() { _ = events.Close() }()

		done := false
		emitDone := func() {
			if !done {
				done = true
				emit(provider.Chunk{Kind: provider.ChunkDone})
			}
		}

		for {
			event, err := events.Next()
			if errors.Is(err, io.EOF) {
				emitDone()
				return
			}
			if err != nil {
				emit(provider.Chunk{Kind: provider.ChunkError, Err: err})
				return
			}
			over, foldErr := folder.Fold(event.Data, emit)
			if foldErr != nil {
				emit(provider.Chunk{Kind: provider.ChunkError, Err: foldErr})
				return
			}
			if over {
				emitDone()
				return
			}
		}
	}()

	return chunks
}
