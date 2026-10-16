package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRunCLIAllUsesOneBaseline(t *testing.T) {
	var stdout bytes.Buffer
	cfg := cliConfig{
		Strategy: "all", Input: "testdata/history.json", MaxTokens: 120,
		KeepRecent: 4, OffloadThreshold: 128, OffloadDir: t.TempDir(),
		PreviewBytes: 120, Output: "json",
	}
	summarizer := fakeSummarizer{text: "受控摘要", usage: Usage{InputTokens: 100, OutputTokens: 8}}
	if err := run(context.Background(), cfg, summarizer, &stdout); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&stdout)
	var reports []Report
	for {
		var report Report
		err := decoder.Decode(&report)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		reports = append(reports, report)
	}
	if len(reports) != 4 {
		t.Fatalf("reports=%d, want 4", len(reports))
	}
	baseline := reports[0].TokensBefore
	for _, report := range reports {
		if report.TokensBefore != baseline || !report.PairingValid {
			t.Fatalf("incomparable report: %+v", report)
		}
	}
}

func TestRunCLISummarizeRequiresSummarizer(t *testing.T) {
	cfg := cliConfig{Strategy: "summarize", Input: "testdata/history.json", KeepRecent: 4, Output: "json"}
	if err := run(context.Background(), cfg, nil, io.Discard); err == nil {
		t.Fatal("summarize accepted a nil summarizer")
	}
}

func TestRunCLITableHasStableColumns(t *testing.T) {
	var stdout bytes.Buffer
	cfg := cliConfig{Strategy: "baseline", Input: "testdata/history.json", Output: "table"}
	if err := run(context.Background(), cfg, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	firstLine, _, _ := strings.Cut(stdout.String(), "\n")
	for _, column := range []string{"strategy", "before", "after", "reduction", "messages", "pairing", "offloaded_bytes", "summary_tokens", "summary_latency"} {
		if !strings.Contains(firstLine, column) {
			t.Fatalf("missing column %q in %q", column, firstLine)
		}
	}
}
