package kafka

import (
	"context"
	"fmt"
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
	brokers []string
	groupID string
	mu      sync.Mutex
	readers map[string]*kg.Reader
}

func NewKafka(brokers []string, groupID string) *Kafka {
	return &Kafka{brokers: brokers, groupID: groupID, readers: map[string]*kg.Reader{}}
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
		fetchCtx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
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
		StartOffset:            kg.FirstOffset,
		WatchPartitionChanges:  true,
		PartitionWatchInterval: time.Second,
	})
	k.readers[topic] = r
	return r
}

func RawTopic(source string) string          { return "xdp.raw." + source }
func OutputTopic(target string) string       { return "xdp.output." + target }
func DeadletterTopic(pipeline string) string { return "xdp.deadletter." + pipeline }
