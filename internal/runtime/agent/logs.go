// Package agent provides agent lifecycle management and abstraction.
package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
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
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if scanner.Scan() {
					line := scanner.Text() + "\n"
					if _, err := pw.Write([]byte(line)); err != nil {
						return
					}
				} else {
					// No more lines, wait a bit before checking again
					select {
					case <-ctx.Done():
						return
					case <-ctx.Done():
						return
					}
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

	// Construct log path from agent ID
	// This assumes logs are stored in a predictable location
	logPath := fmt.Sprintf("/var/log/aether/agents/%s.log", a.info.Config.ID)

	streamer := NewLogStreamer(logPath)
	return streamer.Stream(ctx, follow)
}
