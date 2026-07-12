package kafka

import (
	"testing"

	kg "github.com/segmentio/kafka-go"
)

func TestNewKafkaDefaultsToEarliestOffset(t *testing.T) {
	k := NewKafka([]string{"127.0.0.1:9092"}, "xdp-test")
	if k.startOffset != kg.FirstOffset {
		t.Fatalf("startOffset = %d, want FirstOffset", k.startOffset)
	}
}

func TestNewKafkaAppliesStartOffsetOption(t *testing.T) {
	k := NewKafka([]string{"127.0.0.1:9092"}, "xdp-test", WithStartOffset("latest"))
	if k.startOffset != kg.LastOffset {
		t.Fatalf("startOffset = %d, want LastOffset", k.startOffset)
	}

	k = NewKafka([]string{"127.0.0.1:9092"}, "xdp-test", WithStartOffset("earliest"))
	if k.startOffset != kg.FirstOffset {
		t.Fatalf("startOffset = %d, want FirstOffset", k.startOffset)
	}
}
