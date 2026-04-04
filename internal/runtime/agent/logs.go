// Package agent provides agent lifecycle management and abstraction.
package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// LogStreamer streams logs from an agent.
type LogStreamer struct {
	logPath string
}

// NewLogStreamer creates a new log streamer.
func NewLogStreamer(logPath string) *LogStreamer {
	return &LogStreamer{
		logPath: logPath,
	}
}

// Stream returns a reader for the agent's logs.
// If follow is true, it will continue reading new log lines as they're written.
func (ls *LogStreamer) Stream(ctx context.Context, follow bool) (io.ReadCloser, error) {
	file, err := os.Open(ls.logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	if !follow {
		// Just return the file as-is for one-time read
		return file, nil
	}

	// For follow mode, we need to tail the file
	pr, pw := io.Pipe()

	go func() {
		defer file.Close()
		defer pw.Close()

		scanner := bufio.NewScanner(file)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled - clean exit
				return
			case <-ticker.C:
				// Check for new lines periodically
				for scanner.Scan() {
					line := scanner.Text() + "\n"
					if _, err := pw.Write([]byte(line)); err != nil {
						// Write failed (likely pipe closed) - exit
						return
					}
				}
				// Check for scanner errors
				if err := scanner.Err(); err != nil {
					pw.CloseWithError(fmt.Errorf("scanner error: %w", err))
					return
				}
			}
		}
	}()

	return pr, nil
}

// GetLogs retrieves logs from the agent.
func (a *Agent) GetLogs(ctx context.Context, follow bool) (io.ReadCloser, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	streamer := NewLogStreamer(a.logPath)
	return streamer.Stream(ctx, follow)
}
