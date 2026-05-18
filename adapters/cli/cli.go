package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dydanz/akb48/internal/runtime"
	"github.com/dydanz/akb48/internal/types"
)

// Adapter reads from stdin, writes to stdout, calls the runtime per message.
type Adapter struct {
	reader    *bufio.Scanner
	writer    io.Writer
	handler   runtime.MessageHandler
	sessionID string
}

// New creates a CLI adapter using stdin/stdout and the given message handler.
func New(handler runtime.MessageHandler) *Adapter {
	return &Adapter{
		reader:    bufio.NewScanner(os.Stdin),
		writer:    os.Stdout,
		handler:   handler,
		sessionID: "main:cli:local",
	}
}

// NewWithIO creates a CLI adapter with injected reader/writer (used in tests).
func NewWithIO(reader io.Reader, writer io.Writer, handler runtime.MessageHandler) *Adapter {
	return &Adapter{
		reader:    bufio.NewScanner(reader),
		writer:    writer,
		handler:   handler,
		sessionID: "main:cli:local",
	}
}

// Start runs the read-eval-print loop until EOF or context cancellation.
func (a *Adapter) Start(ctx context.Context) error {
	for {
		fmt.Fprint(a.writer, "> ")

		// Check context before blocking on Scan
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !a.reader.Scan() {
			// EOF or scanner error
			if err := a.reader.Err(); err != nil {
				return err
			}
			return nil
		}

		line := strings.TrimSpace(a.reader.Text())
		if line == "" {
			continue
		}

		tokens := make(chan string, 64)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tokens {
				fmt.Fprint(a.writer, t)
			}
			fmt.Fprintln(a.writer)
		}()

		msg := types.Message{
			SessionID: a.sessionID,
			Text:      line,
			Timestamp: time.Now(),
		}

		if err := a.handler(ctx, msg, tokens); err != nil {
			slog.Error("Handler error", "error", err)
			fmt.Fprintf(a.writer, "Error: %v\n", err)
		}

		close(tokens)
		wg.Wait()
	}
}
