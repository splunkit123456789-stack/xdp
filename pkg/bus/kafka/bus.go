package kafka

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	kg "github.com/segmentio/kafka-go"
)

type Message struct {
	Topic string
	Key   string
	Value []byte
}

type Producer interface {
	Produce(ctx context.Context, msg Message) error
}
type Consumer interface {
	Consume(ctx context.Context, topic string, max int) ([]Message, error)
}

type TopicEnsurer interface {
	EnsureTopic(ctx context.Context, topic string) error
}

type Bus struct {
	mu     sync.Mutex
	topics map[string][]Message
}

func NewBus() *Bus { return &Bus{topics: map[string][]Message{}} }
func (b *Bus) Produce(ctx context.Context, msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.topics[msg.Topic] = append(b.topics[msg.Topic], msg)
	return nil
}
func (b *Bus) EnsureTopic(ctx context.Context, topic string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.topics[topic]; !ok {
		b.topics[topic] = nil
	}
	return nil
}
func (b *Bus) Consume(ctx context.Context, topic string, max int) ([]Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	items := b.topics[topic]
	if max <= 0 || max > len(items) {
		max = len(items)
	}
	out := append([]Message(nil), items[:max]...)
	b.topics[topic] = items[max:]
	return out, nil
}

type Kafka struct {
	brokers     []string
	groupID     string
	startOffset int64
	mu          sync.Mutex
	readers     map[string]*kg.Reader
}

type Option func(*Kafka)

func WithStartOffset(offset string) Option {
	return func(k *Kafka) {
		switch strings.ToLower(strings.TrimSpace(offset)) {
		case "latest":
			k.startOffset = kg.LastOffset
		default:
			k.startOffset = kg.FirstOffset
		}
	}
}

func NewKafka(brokers []string, groupID string, opts ...Option) *Kafka {
	k := &Kafka{brokers: brokers, groupID: groupID, startOffset: kg.FirstOffset, readers: map[string]*kg.Reader{}}
	for _, opt := range opts {
		if opt != nil {
			opt(k)
		}
	}
	return k
}

func (k *Kafka) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	var closeErr error
	for topic, reader := range k.readers {
		if err := reader.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		delete(k.readers, topic)
	}
	return closeErr
}

func (k *Kafka) Produce(ctx context.Context, msg Message) error {
	w := &kg.Writer{Addr: kg.TCP(k.brokers...), Topic: msg.Topic, Balancer: &kg.Hash{}}
	defer w.Close()
	return w.WriteMessages(ctx, kg.Message{Key: []byte(msg.Key), Value: msg.Value, Time: time.Now()})
}
func (k *Kafka) EnsureTopic(ctx context.Context, topic string) error {
	conn, err := kg.DialContext(ctx, "tcp", k.brokers[0])
	if err != nil {
		return err
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	controllerConn, err := kg.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return err
	}
	defer controllerConn.Close()
	return controllerConn.CreateTopics(kg.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
}
func (k *Kafka) Consume(ctx context.Context, topic string, max int) ([]Message, error) {
	r := k.reader(topic)
	out := []Message{}
	if max <= 0 {
		max = 1
	}
	for len(out) < max {
		fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		m, err := r.FetchMessage(fetchCtx)
		cancel()
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			if fetchCtx.Err() != nil {
				return out, nil
			}
			return nil, err
		}
		out = append(out, Message{Topic: topic, Key: string(m.Key), Value: m.Value})
		_ = r.CommitMessages(ctx, m)
	}
	return out, nil
}

func (k *Kafka) reader(topic string) *kg.Reader {
	k.mu.Lock()
	defer k.mu.Unlock()
	if r := k.readers[topic]; r != nil {
		return r
	}
	r := kg.NewReader(kg.ReaderConfig{
		Brokers:                k.brokers,
		Topic:                  topic,
		GroupID:                k.groupID + "." + topic,
		MinBytes:               1,
		MaxBytes:               10e6,
		MaxWait:                500 * time.Millisecond,
		StartOffset:            k.startOffset,
		WatchPartitionChanges:  true,
		PartitionWatchInterval: time.Second,
	})
	k.readers[topic] = r
	return r
}

func RawTopic(source string) string          { return "xdp.raw." + source }
func OutputTopic(target string) string       { return "xdp.output." + target }
func DeadletterTopic(pipeline string) string { return "xdp.deadletter." + pipeline }
