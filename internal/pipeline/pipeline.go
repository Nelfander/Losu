package pipeline

import (
	"context"
	"sync"

	"github.com/nelfander/losu/internal/model"
	"github.com/nelfander/losu/internal/parser"
)

// Process accepts ANY Parser interface
func Process(ctx context.Context, wg *sync.WaitGroup, numWorkers int, p parser.Parser, input <-chan model.RawLog, output chan<- model.LogEvent) {
	for i := 0; i < numWorkers; i++ {
		wg.Add(1) // Tell the WaitGroup a new worker started
		go func() {
			defer wg.Done() // Tell the WaitGroup this worker finished when the loop ends
			for {
				select {
				case <-ctx.Done():
					return
				case rawLine, ok := <-input:
					if !ok {
						return
					}
					// Use the interface method
					output <- p.Parse(rawLine)
				}
			}
		}()
	}
}
