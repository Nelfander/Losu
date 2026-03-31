package tailer

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nelfander/losu/internal/model"
)

type Tailer struct {
	path    string
	results chan<- model.RawLog // send the lines found
}

func NewTailer(path string, results chan<- model.RawLog) *Tailer {
	return &Tailer{
		path:    path,
		results: results,
	}
}
func (t *Tailer) Run(ctx context.Context, changes <-chan struct{}) error {
	file, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Seek to end to start fresh
	_, _ = file.Seek(0, io.SeekEnd)
	reader := bufio.NewReader(file)

	// We'll use a ticker to "force" a check if the signal is being swallowed by the OS
	pollTicker := time.NewTicker(100 * time.Millisecond)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pollTicker.C:
			// This is the "Windows Kick":
			// Calling Stat() forces the OS to update the file size metadata
			_, _ = file.Stat()

			// Now try to drain everything available
			for {
				line, err := reader.ReadString('\n')

				if line != "" {
					cleanLine := strings.TrimSpace(line)
					if cleanLine != "" {
						select {
						case t.results <- model.RawLog{Source: t.path, Line: cleanLine}:
						case <-ctx.Done():
							return ctx.Err()
						default:
							// If the aggregator is full, don't block the tailer
						}
					}
				}

				if err != nil {
					if err == io.EOF {
						break // Back to waiting for the next tick
					}
					return err
				}
			}
		case <-changes:
			// We keep this for reactivity, but the Ticker above is our safety net
		}
	}
}
