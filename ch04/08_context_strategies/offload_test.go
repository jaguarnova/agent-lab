package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOffloadWritesContentAddressedFileAndIsIdempotent(t *testing.T) {
	messages := loadFixture(t)
	dir := t.TempDir()
	first, err := RunOffload(messages, 128, dir, 120)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunOffload(messages, 128, dir, 120)
	if err != nil {
		t.Fatal(err)
	}
	if first.Report.OffloadedFiles == 0 || first.Report.OffloadedBytes == 0 || second.Report.OffloadedFiles != first.Report.OffloadedFiles {
		t.Fatalf("unexpected reports: first=%+v second=%+v", first.Report, second.Report)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != first.Report.OffloadedFiles {
		t.Fatalf("idempotency failed: files=%d report=%d", len(entries), first.Report.OffloadedFiles)
	}
	assertOffloadReferences(t, first.Messages)
}

func TestOffloadFailureDoesNotMutateMessages(t *testing.T) {
	messages := loadFixture(t)
	before, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	fileInsteadOfDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileInsteadOfDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOffload(messages, 128, fileInsteadOfDir, 120); err == nil {
		t.Fatal("expected offload failure")
	}
	after, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("offload failure mutated original messages")
	}
}

func TestOffloadRejectsCorruptExistingContent(t *testing.T) {
	messages := loadFixture(t)
	var content []byte
	for _, message := range messages {
		if message.Role == "tool" && len(message.Content) > 128 {
			content = []byte(message.Content)
			break
		}
	}
	digest := sha256.Sum256(content)
	path := filepath.Join(t.TempDir(), hex.EncodeToString(digest[:])+".txt")
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOffload(messages, 128, filepath.Dir(path), 120); err == nil {
		t.Fatal("corrupt content-addressed file was reused")
	}
}

func assertOffloadReferences(t *testing.T, messages []Message) {
	t.Helper()
	found := 0
	for _, message := range messages {
		if message.Role != "tool" {
			continue
		}
		var reference OffloadReference
		if json.Unmarshal([]byte(message.Content), &reference) != nil || reference.Path == "" {
			continue
		}
		content, err := os.ReadFile(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != reference.SHA256 || len(content) != reference.OriginalBytes {
			t.Fatalf("reference mismatch: %+v", reference)
		}
		found++
	}
	if found == 0 {
		t.Fatal("no offload references found")
	}
}
