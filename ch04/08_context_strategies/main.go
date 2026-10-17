package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
)

type cliConfig struct {
	Strategy         string
	Input            string
	MaxTokens        int
	KeepRecent       int
	OffloadThreshold int
	OffloadDir       string
	PreviewBytes     int
	Output           string
}

func main() {
	cfg := cliConfig{}
	flags := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	flags.StringVar(&cfg.Strategy, "strategy", "baseline", "baseline|truncate|summarize|offload|all")
	flags.StringVar(&cfg.Input, "input", "testdata/history.json", "conversation history JSON")
	flags.IntVar(&cfg.MaxTokens, "max-tokens", 120, "truncate token budget")
	flags.IntVar(&cfg.KeepRecent, "keep-recent", 4, "recent messages retained during summarization")
	flags.IntVar(&cfg.OffloadThreshold, "offload-threshold", 128, "minimum tool-result bytes to offload")
	flags.StringVar(&cfg.OffloadDir, "offload-dir", "", "explicit offload directory")
	flags.IntVar(&cfg.PreviewBytes, "preview-bytes", 120, "bytes retained in each offload reference preview")
	flags.StringVar(&cfg.Output, "output", "table", "table|json")
	_ = flags.Parse(os.Args[1:])

	var summarizer Summarizer
	if cfg.Strategy == "summarize" || cfg.Strategy == "all" {
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is required for summarize/all")
			os.Exit(2)
		}
		summarizer = HTTPSummarizer{
			BaseURL: getenvDefault("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:  apiKey,
			Model:   getenvDefault("MODEL", "deepseek-flash"),
		}
	}
	if err := run(context.Background(), cfg, summarizer, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "context experiment failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg cliConfig, summarizer Summarizer, stdout io.Writer) error {
	if err := validateCLIConfig(cfg, summarizer); err != nil {
		return err
	}
	raw, err := readHistory(cfg.Input)
	if err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	messages, err := DecodeMessages(raw)
	if err != nil {
		return fmt.Errorf("decode history: %w", err)
	}
	if err := ValidatePairing(messages); err != nil {
		return fmt.Errorf("invalid history: %w", err)
	}

	reports := make([]Report, 0, 4)
	appendResult := func(result Result, resultErr error) error {
		if resultErr != nil {
			return resultErr
		}
		reports = append(reports, result.Report)
		return nil
	}
	switch cfg.Strategy {
	case "baseline":
		reports = append(reports, RunBaseline(messages).Report)
	case "truncate":
		if err := appendResult(RunTruncate(messages, cfg.MaxTokens)); err != nil {
			return err
		}
	case "summarize":
		if err := appendResult(RunSummarize(ctx, messages, cfg.KeepRecent, summarizer)); err != nil {
			return err
		}
	case "offload":
		if err := appendResult(RunOffload(messages, cfg.OffloadThreshold, cfg.OffloadDir, cfg.PreviewBytes)); err != nil {
			return err
		}
	case "all":
		reports = append(reports, RunBaseline(messages).Report)
		if err := appendResult(RunTruncate(messages, cfg.MaxTokens)); err != nil {
			return err
		}
		if err := appendResult(RunSummarize(ctx, messages, cfg.KeepRecent, summarizer)); err != nil {
			return err
		}
		if err := appendResult(RunOffload(messages, cfg.OffloadThreshold, cfg.OffloadDir, cfg.PreviewBytes)); err != nil {
			return err
		}
	}
	return writeReports(stdout, cfg.Output, reports)
}

func readHistory(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil || path != "testdata/history.json" || !os.IsNotExist(err) {
		return raw, err
	}
	return os.ReadFile(filepath.Join("ch04", "08_context_strategies", path))
}

func validateCLIConfig(cfg cliConfig, summarizer Summarizer) error {
	switch cfg.Strategy {
	case "baseline":
	case "truncate":
		if cfg.MaxTokens <= 0 {
			return fmt.Errorf("max tokens must be positive")
		}
	case "summarize":
		if summarizer == nil {
			return fmt.Errorf("summarizer is required")
		}
		if cfg.KeepRecent <= 0 {
			return fmt.Errorf("keep recent must be positive")
		}
	case "offload":
		if err := validateOffloadConfig(cfg); err != nil {
			return err
		}
	case "all":
		if summarizer == nil {
			return fmt.Errorf("summarizer is required")
		}
		if cfg.MaxTokens <= 0 || cfg.KeepRecent <= 0 {
			return fmt.Errorf("max tokens and keep recent must be positive")
		}
		if err := validateOffloadConfig(cfg); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported strategy %q", cfg.Strategy)
	}
	if cfg.Input == "" {
		return fmt.Errorf("input path is required")
	}
	if cfg.Output != "table" && cfg.Output != "json" {
		return fmt.Errorf("unsupported output %q", cfg.Output)
	}
	return nil
}

func validateOffloadConfig(cfg cliConfig) error {
	if cfg.OffloadThreshold <= 0 || cfg.PreviewBytes < 0 || cfg.OffloadDir == "" {
		return fmt.Errorf("offload requires a positive threshold, non-negative preview bytes, and an explicit directory")
	}
	return nil
}

func writeReports(destination io.Writer, output string, reports []Report) error {
	if output == "json" {
		encoder := json.NewEncoder(destination)
		for _, report := range reports {
			if err := encoder.Encode(report); err != nil {
				return fmt.Errorf("write JSON report: %w", err)
			}
		}
		return nil
	}
	writer := tabwriter.NewWriter(destination, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "strategy\tbefore\tafter\treduction\tmessages\tpairing\toffloaded_bytes\tsummary_tokens\tsummary_latency"); err != nil {
		return err
	}
	for _, report := range reports {
		if _, err := fmt.Fprintf(writer, "%s\t%d\t%d\t%.1f%%\t%d/%d\t%t\t%d\t%d/%d\t%dms\n",
			report.Strategy, report.TokensBefore, report.TokensAfter, report.ReductionPercent,
			report.MessagesBefore, report.MessagesAfter, report.PairingValid, report.OffloadedBytes,
			report.SummaryInputTokens, report.SummaryOutputTokens, report.SummaryLatencyMillis); err != nil {
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush table report: %w", err)
	}
	return nil
}

func getenvDefault(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if exists {
		return value
	}
	return fallback
}
