package main

import (
	"os"
	"testing"
)

func loadFixture(t *testing.T) []Message {
	t.Helper()
	raw, err := os.ReadFile("testdata/history.json")
	if err != nil {
		t.Fatal(err)
	}
	messages, err := DecodeMessages(raw)
	if err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestBaselineReportsUnchangedHistory(t *testing.T) {
	messages := loadFixture(t)
	got := RunBaseline(messages)
	if got.Report.Strategy != "baseline" || got.Report.TokensBefore != got.Report.TokensAfter || got.Report.MessagesBefore != got.Report.MessagesAfter {
		t.Fatalf("unexpected baseline: %+v", got.Report)
	}
}

func TestTruncatePreservesToolPairing(t *testing.T) {
	messages := loadFixture(t)
	got, err := RunTruncate(messages, 120)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Report.PairingValid || got.Report.TokensAfter >= got.Report.TokensBefore {
		t.Fatalf("unexpected truncation: %+v", got.Report)
	}
	if err := ValidatePairing(got.Messages); err != nil {
		t.Fatal(err)
	}
}

func TestTruncateKeepsMandatoryGroupWhenOverBudget(t *testing.T) {
	messages := loadFixture(t)
	got, err := RunTruncate(messages, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) < 2 || got.Messages[0].Role != "system" || got.Messages[len(got.Messages)-1].Content != "总结结论并保留数据来源。" {
		t.Fatalf("mandatory history missing: %+v", got.Messages)
	}
	if got.Report.TokensAfter <= 1 || !got.Report.PairingValid {
		t.Fatalf("unavoidable overflow not reported honestly: %+v", got.Report)
	}
}

func TestValidatePairingRejectsOrphanedToolResult(t *testing.T) {
	messages := loadFixture(t)
	messages = append([]Message(nil), messages...)
	messages = append(messages[:3], messages[4:]...)
	if err := ValidatePairing(messages); err == nil {
		t.Fatal("orphaned assistant tool call was accepted")
	}
}
