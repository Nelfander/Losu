package tailer

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"

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
	//  Open the file
	file, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Seek to the end (don't read the whole past history)
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(file)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-changes:
			if !ok {
				return nil
			}

			//  Read until the file is empty (EOF).
			for {
				line, err := reader.ReadString('\n')

				// Check for content immediately
				cleanLine := strings.TrimSpace(line)
				if cleanLine != "" {
					t.results <- model.RawLog{
						Source: t.path,
						Line:   cleanLine,
					}
				}

				if err != nil {
					if err == io.EOF {
						// Caught up to the generator, now
						// go back to sleep and wait for the next signal :P
						break
					}
					return err
				}
			}
		}
	}
}
