package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunWritesAnswerBeforeProviderFinishes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	partialWritten := make(chan struct{})
	providerFinished := make(chan struct{})
	newStreamTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(providerFinished)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{
			`{"choices":[{"index":0,"delta":{"reasoning_content":"这只是推理，不是回答"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":" \n"}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"Token 是"}}],"usage":{"prompt_tokens":7,"completion_tokens":1,"total_tokens":8}}`,
		} {
			if err := writeStreamEvent(w, event); err != nil {
				return
			}
		}
		// The provider cannot finish until the command has written visible content.
		select {
		case <-partialWritten:
		case <-r.Context().Done():
			return
		case <-ctx.Done():
			return
		}
		for _, event := range []string{
			`{"choices":[{"index":0,"delta":{"content":"文本单元。"}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}}`,
			`[DONE]`,
		} {
			if err := writeStreamEvent(w, event); err != nil {
				return
			}
		}
	}))

	var output bytes.Buffer
	var once sync.Once
	writer := streamWriterFunc(func(p []byte) (int, error) {
		n, err := output.Write(p)
		if strings.Contains(output.String(), "Token 是") {
			once.Do(func() { close(partialWritten) })
		}
		return n, err
	})
	if err := run(ctx, writer); err != nil {
		t.Fatalf("run: %v", err)
	}
	select {
	case <-providerFinished:
	case <-ctx.Done():
		t.Fatal("provider did not finish after receiving the partial answer")
	}

	text := output.String()
	if !strings.Contains(text, "Token 是文本单元。") {
		t.Fatalf("missing streamed answer: %q", text)
	}
	if strings.Contains(text, "这只是推理") {
		t.Fatalf("reasoning was printed as answer content: %q", text)
	}
	if !strings.Contains(text, "prompt=7 completion=4 total=11") {
		t.Fatalf("usage must retain the last snapshot, not sum snapshots: %q", text)
	}
}

func TestRunRejectsUnfinishedOrEmptyAnswers(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{
			name: "length",
			events: []string{
				`{"choices":[{"index":0,"delta":{"content":"截断的回答"},"finish_reason":"length"}]}`,
				`[DONE]`,
			},
		},
		{
			name: "EOF without stop",
			events: []string{
				`{"choices":[{"index":0,"delta":{"content":"不完整的回答"}}]}`,
			},
		},
		{
			name: "DONE without stop",
			events: []string{
				`{"choices":[{"index":0,"delta":{"content":"没有结束标记"}}]}`,
				`[DONE]`,
			},
		},
		{
			name: "empty answer",
			events: []string{
				`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				`[DONE]`,
			},
		},
		{
			name: "whitespace answer",
			events: []string{
				`{"choices":[{"index":0,"delta":{"content":" \n\t　"},"finish_reason":"stop"}]}`,
				`[DONE]`,
			},
		},
		{
			name: "reasoning is not an answer",
			events: []string{
				`{"choices":[{"index":0,"delta":{"reasoning_content":"只有推理"},"finish_reason":"stop"}]}`,
				`[DONE]`,
			},
		},
		{
			name:   "no chunks",
			events: []string{`[DONE]`},
		},
		{
			name: "tool call despite stop",
			events: []string{
				`{"choices":[{"index":0,"delta":{"content":"还需调用工具","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"add","arguments":"{\"a\":1,\"b\":2}"}}]},"finish_reason":"stop"}]}`,
				`[DONE]`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			newStreamTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range test.events {
					if err := writeStreamEvent(w, event); err != nil {
						return
					}
				}
			}))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(cancel)
			var output bytes.Buffer
			if err := run(ctx, &output); err == nil {
				t.Fatalf("run accepted an unfinished or empty answer: %q", output.String())
			}
			if strings.Contains(output.String(), "只有推理") {
				t.Fatalf("reasoning-only content was exposed as an answer: %q", output.String())
			}
		})
	}
}

func TestRunClosesProviderOnOutputErrorOrCancellation(t *testing.T) {
	outputErr := errors.New("output unavailable")
	tests := []struct {
		name    string
		wantErr error
	}{
		{name: "output error", wantErr: outputErr},
		{name: "cancellation", wantErr: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			waitCtx, stopWaiting := context.WithTimeout(t.Context(), 5*time.Second)
			t.Cleanup(stopWaiting)
			ctx, cancel := context.WithCancel(waitCtx)
			t.Cleanup(cancel)
			providerClosed := make(chan struct{})
			newStreamTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if err := writeStreamEvent(w, `{"choices":[{"index":0,"delta":{"content":"部分回答"}}]}`); err != nil {
					return
				}
				select {
				case <-r.Context().Done():
					close(providerClosed)
				case <-waitCtx.Done():
				}
			}))
			writer := streamWriterFunc(func(p []byte) (int, error) {
				if bytes.Contains(p, []byte("部分回答")) {
					if test.wantErr == outputErr {
						return 0, outputErr
					}
					cancel()
				}
				return len(p), nil
			})
			if err := run(ctx, writer); !errors.Is(err, test.wantErr) {
				t.Fatalf("run error = %v, want %v", err, test.wantErr)
			}
			select {
			case <-providerClosed:
			case <-waitCtx.Done():
				t.Fatal("run returned without closing the provider request")
			}
		})
	}
}

type streamWriterFunc func([]byte) (int, error)

func (f streamWriterFunc) Write(p []byte) (int, error) {
	return f(p)
}

func newStreamTestServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
	})
	t.Setenv("DEEPSEEK_API_KEY", "test-key-not-a-secret")
	t.Setenv("DEEPSEEK_BASE_URL", server.URL)
	t.Setenv("MODEL", "test-model")
}

func writeStreamEvent(w http.ResponseWriter, event string) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}
