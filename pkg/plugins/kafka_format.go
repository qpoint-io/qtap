package plugins

import (
	"fmt"
	"strings"
)

// MaxKafkaSummaryBytes is the maximum byte length of a Kafka response summary.
const MaxKafkaSummaryBytes = 64 * 1024 // 64KB

// BuildKafkaStatement creates a human-readable statement from a KafkaCommand.
func BuildKafkaStatement(cmd *KafkaCommand) string {
	var parts []string
	parts = append(parts, cmd.Operation)
	if len(cmd.Topics) > 0 {
		parts = append(parts, strings.Join(cmd.Topics, ","))
	}
	if cmd.GroupID != "" {
		parts = append(parts, "group="+cmd.GroupID)
	}
	return strings.Join(parts, " ")
}

// BuildKafkaResponseSummary creates a summary of topics and message samples
// from a KafkaCommand and optional KafkaResult.
func BuildKafkaResponseSummary(cmd *KafkaCommand, res *KafkaResult) string {
	var sb strings.Builder

	// Collect all messages (from command for Produce, from result for Fetch)
	var messages []KafkaMessage
	if len(cmd.Messages) > 0 {
		messages = cmd.Messages
	}
	if res != nil && len(res.Messages) > 0 {
		messages = append(messages, res.Messages...)
	}

	if len(messages) == 0 {
		if len(cmd.Topics) > 0 {
			sb.WriteString("topics: " + strings.Join(cmd.Topics, ", "))
		}
		return truncateKafkaSummary(sb.String())
	}

	fmt.Fprintf(&sb, "%d messages", len(messages))
	for i, msg := range messages {
		if i >= 10 { // Cap at 10 message samples
			fmt.Fprintf(&sb, "\n... and %d more", len(messages)-10)
			break
		}
		fmt.Fprintf(&sb, "\n  [%s/%d]", msg.Topic, msg.Partition)
		if msg.Key != "" {
			sb.WriteString(" key=" + msg.Key)
		}
		if msg.Value != "" {
			val := msg.Value
			if runes := []rune(val); len(runes) > 256 {
				val = string(runes[:256]) + "..."
			}
			sb.WriteString(" " + val)
		}
	}

	return truncateKafkaSummary(sb.String())
}

// truncateKafkaSummary truncates s to MaxKafkaSummaryBytes at a valid UTF-8 boundary.
// It backs up past any UTF-8 continuation bytes (10xxxxxx) to land on a leading byte.
func truncateKafkaSummary(s string) string {
	if len(s) <= MaxKafkaSummaryBytes {
		return s
	}
	i := MaxKafkaSummaryBytes
	for i > 0 && (s[i]&0xC0) == 0x80 {
		i--
	}
	return s[:i]
}
