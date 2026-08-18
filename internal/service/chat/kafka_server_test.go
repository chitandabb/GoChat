package chat

import (
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestPersistBatchMalformedJSONClearsInFlightAndCommits(t *testing.T) {
	var committed []kafka.Message
	server := &KafkaServer{
		inFlightOffsets: make(map[int]map[int64]struct{}),
		commitFn: func(messages []kafka.Message) error {
			committed = append(committed, messages...)
			return nil
		},
		markDoneFn: func(string) {},
	}
	message := kafka.Message{
		Topic:     "chat_message",
		Partition: 2,
		Offset:    41,
		Time:      time.UnixMilli(1700000000000),
		Value:     []byte(`{"type":`),
	}
	server.registerInFlight(message)

	if failed := server.persistBatch([]kafka.Message{message}); len(failed) != 0 {
		t.Fatalf("malformed message must not be retried, got %d failed messages", len(failed))
	}
	if offsets := server.inFlightOffsets[message.Partition]; len(offsets) != 0 {
		t.Fatalf("in-flight offsets not cleared: %#v", offsets)
	}
	if len(committed) != 1 || committed[0].Partition != message.Partition || committed[0].Offset != message.Offset {
		t.Fatalf("malformed message was not committed: %#v", committed)
	}
}
