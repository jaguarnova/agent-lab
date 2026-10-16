package main

import "fmt"

type messageGroup struct {
	start int
	end   int
}

func RunBaseline(messages []Message) Result {
	cloned := cloneMessages(messages)
	return Result{Messages: cloned, Report: newReport("baseline", messages, cloned, 0)}
}

func RunTruncate(messages []Message, maxTokens int) (Result, error) {
	if maxTokens <= 0 {
		return Result{}, fmt.Errorf("max tokens must be positive")
	}
	if err := ValidatePairing(messages); err != nil {
		return Result{}, err
	}
	if EstimateMessages(messages) <= maxTokens {
		cloned := cloneMessages(messages)
		return Result{Messages: cloned, Report: newReport("truncate", messages, cloned, 0)}, nil
	}

	firstGroupIndex := 0
	mandatoryRunes := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		firstGroupIndex = 1
		mandatoryRunes = messageRunes(messages[:1])
	}
	groups := atomicGroups(messages, firstGroupIndex)
	if len(groups) == 0 {
		cloned := cloneMessages(messages[:firstGroupIndex])
		return Result{Messages: cloned, Report: newReport("truncate", messages, cloned, len(messages)-len(cloned))}, nil
	}

	selected := len(groups) - 1
	newest := groups[selected]
	totalRunes := mandatoryRunes + messageRunes(messages[newest.start:newest.end])
	for index := selected - 1; index >= 0; index-- {
		group := groups[index]
		candidateRunes := totalRunes + messageRunes(messages[group.start:group.end])
		if (candidateRunes+1)/2 > maxTokens {
			break
		}
		selected = index
		totalRunes = candidateRunes
	}

	keptMessages := firstGroupIndex
	for _, group := range groups[selected:] {
		keptMessages += group.end - group.start
	}
	out := make([]Message, 0, keptMessages)
	if firstGroupIndex == 1 {
		out = appendClonedMessages(out, messages[:1])
	}
	for _, group := range groups[selected:] {
		out = appendClonedMessages(out, messages[group.start:group.end])
	}
	return Result{
		Messages: out,
		Report:   newReport("truncate", messages, out, len(messages)-len(out)),
	}, nil
}

func ValidatePairing(messages []Message) error {
	for index := 0; index < len(messages); index++ {
		message := messages[index]
		if message.Role == "tool" {
			return fmt.Errorf("tool message %d is not immediately paired with an assistant call", index)
		}
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}

		expected := make(map[string]struct{}, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				return fmt.Errorf("assistant message %d contains an empty tool call ID", index)
			}
			if _, duplicate := expected[call.ID]; duplicate {
				return fmt.Errorf("assistant message %d repeats tool call ID %q", index, call.ID)
			}
			expected[call.ID] = struct{}{}
		}
		for offset := 1; offset <= len(message.ToolCalls); offset++ {
			resultIndex := index + offset
			if resultIndex >= len(messages) || messages[resultIndex].Role != "tool" {
				return fmt.Errorf("assistant message %d is missing an immediate tool result", index)
			}
			resultID := messages[resultIndex].ToolCallID
			if _, ok := expected[resultID]; !ok {
				return fmt.Errorf("tool message %d has undeclared call ID %q", resultIndex, resultID)
			}
			delete(expected, resultID)
		}
		if len(expected) != 0 {
			return fmt.Errorf("assistant message %d is missing tool results", index)
		}
		index += len(message.ToolCalls)
	}
	return nil
}

func atomicGroups(messages []Message, start int) []messageGroup {
	groups := make([]messageGroup, 0, len(messages)-start)
	for index := start; index < len(messages); {
		end := index + 1
		if messages[index].Role == "assistant" && len(messages[index].ToolCalls) > 0 {
			end += len(messages[index].ToolCalls)
		}
		groups = append(groups, messageGroup{start: index, end: end})
		index = end
	}
	return groups
}

func cloneMessages(messages []Message) []Message {
	return appendClonedMessages(make([]Message, 0, len(messages)), messages)
}

func appendClonedMessages(destination, messages []Message) []Message {
	for _, message := range messages {
		message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
		destination = append(destination, message)
	}
	return destination
}
