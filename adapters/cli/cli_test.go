package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dydanz/akb48/internal/types"
)

func noopHandler(_ context.Context, _ types.Message, tokens chan<- string) error {
	tokens <- "ok"
	return nil
}

func echoHandler(_ context.Context, msg types.Message, tokens chan<- string) error {
	tokens <- "echo:" + msg.Text
	return nil
}

func captureHandler(captures *[]types.Message) func(context.Context, types.Message, chan<- string) error {
	return func(_ context.Context, msg types.Message, tokens chan<- string) error {
		*captures = append(*captures, msg)
		tokens <- "captured"
		return nil
	}
}

func TestCLIAdapter_SingleMessage(t *testing.T) {
	input := strings.NewReader("hello\n")
	var output bytes.Buffer
	var called bool

	handler := func(_ context.Context, msg types.Message, tokens chan<- string) error {
		called = true
		if msg.Text != "hello" {
			t.Errorf("unexpected message text: %q", msg.Text)
		}
		tokens <- "world"
		return nil
	}

	a := NewWithIO(input, &output, handler)
	a.Start(context.Background())

	if !called {
		t.Error("handler was not called")
	}
	if !strings.Contains(output.String(), "world") {
		t.Errorf("output missing 'world': %q", output.String())
	}
}

func TestCLIAdapter_EmptyLineSkipped(t *testing.T) {
	input := strings.NewReader("\nhello\n")
	var output bytes.Buffer
	callCount := 0

	handler := func(_ context.Context, _ types.Message, tokens chan<- string) error {
		callCount++
		tokens <- "resp"
		return nil
	}

	a := NewWithIO(input, &output, handler)
	a.Start(context.Background())

	if callCount != 1 {
		t.Errorf("expected handler called once, got %d", callCount)
	}
}

func TestCLIAdapter_MultipleMessages(t *testing.T) {
	input := strings.NewReader("first\nsecond\nthird\n")
	var output bytes.Buffer
	var captured []types.Message

	a := NewWithIO(input, &output, captureHandler(&captured))
	a.Start(context.Background())

	if len(captured) != 3 {
		t.Errorf("expected 3 messages, got %d", len(captured))
	}
	if captured[0].Text != "first" || captured[1].Text != "second" || captured[2].Text != "third" {
		t.Errorf("unexpected messages: %+v", captured)
	}
}

func TestCLIAdapter_EchoOutput(t *testing.T) {
	input := strings.NewReader("ping\n")
	var output bytes.Buffer

	a := NewWithIO(input, &output, echoHandler)
	a.Start(context.Background())

	if !strings.Contains(output.String(), "echo:ping") {
		t.Errorf("expected echo:ping in output, got %q", output.String())
	}
}

func TestCLIAdapter_SessionID(t *testing.T) {
	input := strings.NewReader("test\n")
	var output bytes.Buffer
	var capturedSessionID string

	handler := func(_ context.Context, msg types.Message, tokens chan<- string) error {
		capturedSessionID = msg.SessionID
		tokens <- "ok"
		return nil
	}

	a := NewWithIO(input, &output, handler)
	a.Start(context.Background())

	if capturedSessionID != "main:cli:local" {
		t.Errorf("expected session ID 'main:cli:local', got %q", capturedSessionID)
	}
}

func TestCLIAdapter_ContextCancel(t *testing.T) {
	// Block until context cancelled
	r, w := bytes.NewReader([]byte{}), &bytes.Buffer{}
	_ = r

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	a := NewWithIO(strings.NewReader(""), w, noopHandler)
	start := time.Now()
	a.Start(ctx)

	if time.Since(start) > 500*time.Millisecond {
		t.Error("Start should return quickly on empty input")
	}
}

func TestCLIAdapter_StreamingTokens(t *testing.T) {
	input := strings.NewReader("stream\n")
	var output bytes.Buffer

	handler := func(_ context.Context, _ types.Message, tokens chan<- string) error {
		// Simulate streaming: multiple token writes
		tokens <- "tok1"
		tokens <- " "
		tokens <- "tok2"
		return nil
	}

	a := NewWithIO(input, &output, handler)
	a.Start(context.Background())

	out := output.String()
	if !strings.Contains(out, "tok1") || !strings.Contains(out, "tok2") {
		t.Errorf("expected streaming tokens in output, got %q", out)
	}
}

func TestCLIAdapter_PromptShown(t *testing.T) {
	input := strings.NewReader("hi\n")
	var output bytes.Buffer

	a := NewWithIO(input, &output, noopHandler)
	a.Start(context.Background())

	if !strings.Contains(output.String(), "> ") {
		t.Errorf("expected '> ' prompt in output, got %q", output.String())
	}
}
