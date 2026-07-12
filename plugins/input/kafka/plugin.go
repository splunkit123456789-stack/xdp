package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	buskafka "xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Input struct {
	mu          sync.RWMutex
	cfg         Config
	consumer    buskafka.Consumer
	cancel      context.CancelFunc
	status      string
	listener    string
	loadedAt    time.Time
	lastRecvAt  time.Time
	lastErr     string
	eventsTotal uint64
	bytesTotal  uint64
}

type Config struct {
	Brokers          []string
	Topic            string
	ConsumerGroup    string
	StartOffset      string
	SecurityProtocol string
	Encoding         string
	LogFilterEnabled bool
	LogFilterRegex   *regexp.Regexp
	LogFilterPattern string
	Name             string
	DataSourceID     string
	PluginVersion    string
	ConfigVersion    int64
	PollBatchSize    int
	PollInterval     time.Duration
}

func New() *Input { return &Input{} }

func NewWithConsumer(consumer buskafka.Consumer) *Input {
	return &Input{consumer: consumer}
}

func (i *Input) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "kafka",
		Name:        "Kafka Input",
		Type:        plugin.TypeInput,
		Version:     "1.0.0",
		Description: "Consume raw events from Kafka topics",
		Runtime:     "go_builtin",
		Labels: map[string]string{
			"phase":           "P1",
			"runtime_ingest":  "true",
			"transport":       "tcp",
			"supports_reload": "true",
		},
		ConfigSchema: plugin.Schema{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled"},
			"properties": map[string]any{
				"brokers":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
				"topic":              map[string]any{"type": "string", "minLength": 1},
				"consumer_group":     map[string]any{"type": "string", "minLength": 1},
				"start_offset":       map[string]any{"type": "string", "enum": []string{"earliest", "latest"}},
				"security_protocol":  map[string]any{"type": "string", "enum": []string{"PLAINTEXT", "SASL_PLAINTEXT", "SASL_SSL", "SSL"}},
				"encoding":           map[string]any{"type": "string", "enum": []string{"UTF-8", "GBK", "ISO-8859-1"}},
				"log_filter_enabled": map[string]any{"type": "boolean"},
				"log_filter_regex":   map[string]any{"type": "string", "x-required-if": map[string]any{"field": "log_filter_enabled", "equals": true}},
				"source_name":        map[string]any{"type": "string"},
				"data_source_id":     map[string]any{"type": "string"},
				"config_version":     map[string]any{"type": "integer"},
			},
		},
		UISchema: plugin.Schema{
			"order": []string{"brokers", "topic", "consumer_group", "start_offset", "security_protocol", "encoding", "log_filter_enabled", "log_filter_regex"},
		},
		OutputSchema: plugin.Schema{
			"type": "object",
			"metadata": map[string]any{
				"data_source_id":   "string",
				"data_source_name": "string",
				"plugin_code":      "kafka",
				"plugin_version":   "string",
				"config_version":   "integer",
				"kafka_topic":      "string",
				"kafka_brokers":    "array",
			},
		},
	}
}

func (i *Input) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}

func (i *Input) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	if cfg.PluginVersion == "" {
		cfg.PluginVersion = ctx.PluginVersion()
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.cfg = cfg
	if i.consumer == nil {
		i.consumer = buskafka.NewKafka(cfg.Brokers, cfg.ConsumerGroup, buskafka.WithStartOffset(cfg.StartOffset))
	}
	i.status = "initialized"
	i.listener = "stopped"
	i.loadedAt = time.Now().UTC()
	i.lastErr = ""
	return nil
}

func (i *Input) Start(ctx context.Context, emit plugin.EmitFunc) error {
	i.mu.Lock()
	if i.cancel != nil {
		i.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	i.cancel = cancel
	i.status = "running"
	i.listener = "consuming"
	i.loadedAt = time.Now().UTC()
	i.lastErr = ""
	startedCfg := i.cfg
	i.mu.Unlock()
	slog.Info("kafka input loop started", "source", startedCfg.Name, "topic", startedCfg.Topic, "group", startedCfg.ConsumerGroup, "brokers", strings.Join(startedCfg.Brokers, ","))

	defer func() {
		i.closeConsumer()
		i.mu.Lock()
		i.cancel = nil
		if i.status != "failed" {
			i.status = "stopped"
		}
		i.listener = "stopped"
		i.mu.Unlock()
	}()

	for {
		if runCtx.Err() != nil {
			return nil
		}
		i.mu.RLock()
		cfg := i.cfg
		consumer := i.consumer
		i.mu.RUnlock()
		if consumer == nil {
			err := fmt.Errorf("kafka input is not initialized")
			i.recordError(err)
			return err
		}
		messages, err := consumer.Consume(runCtx, cfg.Topic, cfg.PollBatchSize)
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			slog.Warn("kafka input consume failed", "source", cfg.Name, "topic", cfg.Topic, "group", cfg.ConsumerGroup, "error", err)
			i.recordError(err)
			time.Sleep(cfg.PollInterval)
			continue
		}
		if len(messages) == 0 {
			time.Sleep(cfg.PollInterval)
			continue
		}
		for _, msg := range messages {
			raw := strings.TrimSpace(string(msg.Value))
			if raw == "" {
				continue
			}
			slog.Info("kafka input message received", "source", cfg.Name, "topic", msg.Topic, "key", msg.Key, "bytes", len(msg.Value))
			if cfg.LogFilterEnabled && cfg.LogFilterRegex != nil && !cfg.LogFilterRegex.MatchString(raw) {
				slog.Info("kafka input message filtered", "source", cfg.Name, "topic", msg.Topic, "key", msg.Key)
				continue
			}
			ev := event.New(raw, event.Source{Type: "kafka", Name: cfg.Name}, time.Now().UTC())
			ev.Metadata["source_name"] = cfg.Name
			ev.Metadata["data_source_name"] = cfg.Name
			ev.Metadata["data_source_id"] = cfg.DataSourceID
			ev.Metadata["plugin_code"] = "kafka"
			ev.Metadata["plugin_version"] = cfg.PluginVersion
			ev.Metadata["config_version"] = cfg.ConfigVersion
			ev.Metadata["kafka_topic"] = msg.Topic
			ev.Metadata["kafka_key"] = msg.Key
			ev.Metadata["kafka_brokers"] = cfg.Brokers
			if err := emit(runCtx, ev); err != nil {
				i.recordError(err)
				continue
			}
			i.recordReceived(len(msg.Value))
		}
	}
}

func (i *Input) Stop(ctx context.Context) error {
	i.mu.Lock()
	if i.cancel != nil {
		i.cancel()
		i.cancel = nil
	}
	if i.status == "" {
		i.status = "initialized"
	}
	if i.status != "failed" {
		i.status = "stopped"
	}
	i.listener = "stopped"
	i.mu.Unlock()
	i.closeConsumer()
	return nil
}

func (i *Input) Reload(ctx context.Context, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	wasRunning := false
	i.mu.RLock()
	wasRunning = i.cancel != nil
	i.mu.RUnlock()
	if wasRunning {
		_ = i.Stop(ctx)
	}
	i.closeConsumer()
	i.mu.Lock()
	i.cfg = cfg
	i.consumer = buskafka.NewKafka(cfg.Brokers, cfg.ConsumerGroup, buskafka.WithStartOffset(cfg.StartOffset))
	i.status = "initialized"
	i.listener = "stopped"
	i.loadedAt = time.Now().UTC()
	i.lastErr = ""
	i.mu.Unlock()
	return nil
}

func (i *Input) Health(ctx context.Context) plugin.HealthStatus {
	i.mu.RLock()
	defer i.mu.RUnlock()
	status := i.status
	if status == "" {
		status = "new"
	}
	listener := i.listener
	if listener == "" {
		listener = "stopped"
	}
	return plugin.HealthStatus{
		Status:              status,
		ListenerStatus:      listener,
		Endpoint:            i.cfg.endpoint(),
		ReceivedEventsTotal: atomic.LoadUint64(&i.eventsTotal),
		ReceivedBytesTotal:  atomic.LoadUint64(&i.bytesTotal),
		LastReceivedAt:      i.lastRecvAt,
		LastLoadedAt:        i.loadedAt,
		LastError:           i.lastErr,
		Metadata: map[string]any{
			"data_source_id":   i.cfg.DataSourceID,
			"data_source_name": i.cfg.Name,
			"plugin_code":      "kafka",
			"plugin_version":   i.cfg.PluginVersion,
			"config_version":   i.cfg.ConfigVersion,
			"kafka_topic":      i.cfg.Topic,
			"kafka_brokers":    i.cfg.Brokers,
			"consumer_group":   i.cfg.ConsumerGroup,
		},
	}
}

func (i *Input) Close() error { return i.Stop(context.Background()) }

func (i *Input) closeConsumer() {
	i.mu.RLock()
	consumer := i.consumer
	i.mu.RUnlock()
	closer, ok := consumer.(interface{ Close() error })
	if !ok || closer == nil {
		return
	}
	if err := closer.Close(); err != nil {
		slog.Warn("kafka input consumer close failed", "error", err)
	}
}

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{
		StartOffset:      "earliest",
		SecurityProtocol: "PLAINTEXT",
		Encoding:         "UTF-8",
		PluginVersion:    "1.0.0",
		ConfigVersion:    1,
		PollBatchSize:    10,
		PollInterval:     200 * time.Millisecond,
	}
	cfg.Brokers = stringSlice(config["brokers"])
	cfg.Topic = strings.TrimSpace(stringValue(config, "topic", ""))
	cfg.ConsumerGroup = strings.TrimSpace(stringValue(config, "consumer_group", ""))
	cfg.StartOffset = strings.ToLower(strings.TrimSpace(stringValue(config, "start_offset", cfg.StartOffset)))
	cfg.SecurityProtocol = strings.ToUpper(strings.TrimSpace(stringValue(config, "security_protocol", cfg.SecurityProtocol)))
	cfg.Encoding = strings.ToUpper(strings.TrimSpace(stringValue(config, "encoding", cfg.Encoding)))
	cfg.LogFilterEnabled = boolValue(config["log_filter_enabled"])
	cfg.Name = strings.TrimSpace(stringValue(config, "source_name", stringValue(config, "name", "xdp-agent-kafka")))
	cfg.DataSourceID = strings.TrimSpace(stringValue(config, "data_source_id", ""))
	cfg.PluginVersion = strings.TrimSpace(stringValue(config, "plugin_version", cfg.PluginVersion))
	if v, ok := int64Value(config["config_version"]); ok {
		cfg.ConfigVersion = v
	}
	if v, ok := intValue(config["poll_batch_size"]); ok && v > 0 {
		cfg.PollBatchSize = v
	}
	if text := strings.TrimSpace(stringValue(config, "poll_interval", "")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			return cfg, fmt.Errorf("poll_interval must be a duration: %w", err)
		}
		cfg.PollInterval = parsed
	}
	if len(cfg.Brokers) == 0 {
		return cfg, fmt.Errorf("brokers is required")
	}
	if cfg.Topic == "" {
		return cfg, fmt.Errorf("topic is required")
	}
	if cfg.ConsumerGroup == "" {
		return cfg, fmt.Errorf("consumer_group is required")
	}
	if cfg.StartOffset != "earliest" && cfg.StartOffset != "latest" {
		return cfg, fmt.Errorf("start_offset must be earliest or latest")
	}
	if cfg.SecurityProtocol != "PLAINTEXT" && cfg.SecurityProtocol != "SASL_PLAINTEXT" && cfg.SecurityProtocol != "SASL_SSL" && cfg.SecurityProtocol != "SSL" {
		return cfg, fmt.Errorf("security_protocol is invalid")
	}
	if cfg.Encoding == "" {
		return cfg, fmt.Errorf("encoding is required")
	}
	if cfg.LogFilterEnabled {
		pattern := strings.TrimSpace(stringValue(config, "log_filter_regex", ""))
		if pattern == "" {
			return cfg, fmt.Errorf("log_filter_regex is required when log filtering is enabled")
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return cfg, fmt.Errorf("log_filter_regex is invalid: %w", err)
		}
		cfg.LogFilterPattern = pattern
		cfg.LogFilterRegex = compiled
	}
	return cfg, nil
}

func (c Config) endpoint() string {
	if len(c.Brokers) == 0 || c.Topic == "" {
		return ""
	}
	return fmt.Sprintf("kafka://%s/%s", strings.Join(c.Brokers, ","), c.Topic)
}

func (i *Input) recordReceived(n int) {
	atomic.AddUint64(&i.eventsTotal, 1)
	atomic.AddUint64(&i.bytesTotal, uint64(n))
	i.mu.Lock()
	i.lastRecvAt = time.Now().UTC()
	i.mu.Unlock()
}

func (i *Input) recordError(err error) {
	i.mu.Lock()
	i.lastErr = err.Error()
	i.mu.Unlock()
}

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return compact(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			items = append(items, fmt.Sprint(item))
		}
		return compact(items)
	case string:
		return compact(strings.Split(typed, ","))
	default:
		return nil
	}
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func stringValue(config map[string]any, key string, fallback string) string {
	if config == nil {
		return fallback
	}
	value, ok := config[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}
	return text
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.EqualFold(strings.TrimSpace(typed), "on")
	default:
		return false
	}
}

func intValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		n, err := typed.Int64()
		return int(n), err == nil
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		n, err := typed.Int64()
		return n, err == nil
	case string:
		var n int64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}
