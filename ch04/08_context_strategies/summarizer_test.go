package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeSummarizer struct {
	text  string
	usage Usage
	err   error
}

func (f fakeSummarizer) Summarize(context.Context, []Message) (string, Usage, error) {
	return f.text, f.usage, f.err
}

func TestRunSummarizeKeepsRecentHistory(t *testing.T) {
	messages := loadFixture(t)
	got, err := RunSummarize(context.Background(), messages, 4, fakeSummarizer{
		text:  "早期销售查询摘要",
		usage: Usage{InputTokens: 100, OutputTokens: 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages[1].Role != "system" || got.Messages[1].Content != "[history summary]\n早期销售查询摘要" {
		t.Fatalf("missing summary message: %+v", got.Messages)
	}
	if got.Report.SummaryInputTokens != 100 || got.Report.MessagesAfter >= got.Report.MessagesBefore || !got.Report.PairingValid {
		t.Fatalf("unexpected report: %+v", got.Report)
	}
}

func TestRunSummarizeDoesNotFallbackOnError(t *testing.T) {
	_, err := RunSummarize(context.Background(), loadFixture(t), 4, fakeSummarizer{err: errors.New("provider unavailable")})
	if err == nil {
		t.Fatal("summary error was hidden")
	}
}

func TestControlledSummaryReport(t *testing.T) {
	got, err := RunSummarize(context.Background(), loadFixture(t), 4, fakeSummarizer{
		text:  "受控摘要：华南 6620.00，高于华东 5700.50；amount 为 numeric(12,2)。",
		usage: Usage{InputTokens: 100, OutputTokens: 24, Latency: 5 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got.Report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("controlled summarizer fixture: %s", raw)
}

func TestHTTPSummarizerReturnsContentAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"压缩后的事实"}}],"usage":{"prompt_tokens":21,"completion_tokens":7}}`)
	}))
	t.Cleanup(server.Close)
	summarizer := HTTPSummarizer{BaseURL: server.URL, APIKey: "test", Model: "test", Client: server.Client()}
	text, usage, err := summarizer.Summarize(context.Background(), loadFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if text != "压缩后的事实" || usage.InputTokens != 21 || usage.OutputTokens != 7 {
		t.Fatalf("text=%q usage=%+v", text, usage)
	}
}

func TestHTTPSummarizerRejectsInvalidResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http error", status: http.StatusInternalServerError, body: "boom"},
		{name: "malformed json", status: http.StatusOK, body: "{"},
		{name: "missing choice", status: http.StatusOK, body: `{"choices":[]}`},
		{name: "empty content", status: http.StatusOK, body: `{"choices":[{"message":{"content":""}}]}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = io.WriteString(w, testCase.body)
			}))
			t.Cleanup(server.Close)
			summarizer := HTTPSummarizer{BaseURL: server.URL, APIKey: "test", Model: "test", Client: server.Client()}
			if _, _, err := summarizer.Summarize(context.Background(), loadFixture(t)); err == nil {
				t.Fatal("invalid provider response was accepted")
			}
		})
	}
}
