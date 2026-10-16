package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

type OffloadReference struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	OriginalBytes int    `json:"original_bytes"`
	Preview       string `json:"preview"`
}

func RunOffload(messages []Message, threshold int, dir string, previewBytes int) (Result, error) {
	if threshold <= 0 {
		return Result{}, fmt.Errorf("offload threshold must be positive")
	}
	if dir == "" {
		return Result{}, fmt.Errorf("offload directory is required")
	}
	if previewBytes < 0 {
		return Result{}, fmt.Errorf("preview bytes must not be negative")
	}
	if err := ValidatePairing(messages); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create offload directory: %w", err)
	}

	out := cloneMessages(messages)
	uniqueFiles := make(map[string]struct{})
	offloadedBytes := int64(0)
	offloadedMessages := 0
	for index := range out {
		if out[index].Role != "tool" || len([]byte(out[index].Content)) <= threshold {
			continue
		}
		content := []byte(out[index].Content)
		digest := sha256.Sum256(content)
		digestText := hex.EncodeToString(digest[:])
		path := filepath.Join(dir, digestText+".txt")
		if err := writeContentAddressed(path, content, digest); err != nil {
			return Result{}, err
		}
		reference := OffloadReference{
			Path:          path,
			SHA256:        digestText,
			OriginalBytes: len(content),
			Preview:       safePreview(content, previewBytes),
		}
		replacement, err := json.Marshal(reference)
		if err != nil {
			return Result{}, fmt.Errorf("encode offload reference: %w", err)
		}
		out[index].Content = string(replacement)
		offloadedBytes += int64(len(content))
		offloadedMessages++
		uniqueFiles[digestText] = struct{}{}
	}
	if err := ValidatePairing(out); err != nil {
		return Result{}, fmt.Errorf("offloaded history is invalid: %w", err)
	}
	report := newReport("offload", messages, out, offloadedMessages)
	report.OffloadedBytes = offloadedBytes
	report.OffloadedFiles = len(uniqueFiles)
	return Result{Messages: out, Report: report}, nil
}

func writeContentAddressed(path string, content []byte, expected [sha256.Size]byte) error {
	if err := verifyContentAddressed(path, expected); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".offload-*")
	if err != nil {
		return fmt.Errorf("create temporary offload: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write temporary offload: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary offload: %w", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary offload: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, path); err != nil {
		if verifyErr := verifyContentAddressed(path, expected); verifyErr == nil {
			return nil
		}
		return fmt.Errorf("install offload file: %w", err)
	}
	return nil
}

func verifyContentAddressed(path string, expected [sha256.Size]byte) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(content)
	if actual != expected {
		return fmt.Errorf("existing offload file %q has unexpected content", path)
	}
	return nil
}

func safePreview(content []byte, limit int) string {
	if limit >= len(content) {
		return string(content)
	}
	preview := content[:limit]
	for len(preview) > 0 && !utf8.Valid(preview) {
		preview = preview[:len(preview)-1]
	}
	return string(preview)
}
