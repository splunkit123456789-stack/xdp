package syslog

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Input struct {
	mu          sync.RWMutex
	cfg         Config
	conn        net.PacketConn
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
	Addr             string
	Protocol         string
	Name             string
	Sourcetype       string
	Encoding         string
	LogFilterEnabled bool
	LogFilterRegex   *regexp.Regexp
	LogFilterPattern string
	DataSourceID     string
	PluginVersion    string
	ConfigVersion    int64
}

func New() *Input { return &Input{} }

func (i *Input) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "syslog",
		Name:        "Syslog Input",
		Type:        plugin.TypeInput,
		Version:     "1.0.0",
		Description: "Receive raw syslog events over UDP",
		Runtime:     "go_builtin",
		Labels: map[string]string{
			"phase":           "P0",
			"runtime_ingest":  "true",
			"transport":       "udp",
			"connectivity":    "port-check",
			"supports_reload": "true",
		},
		ConfigSchema: plugin.Schema{
			"type":     "object",
			"required": []string{"collector_port", "transport_protocol", "encoding", "log_filter_enabled"},
			"properties": map[string]any{
				"collector_port":     map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
				"transport_protocol": map[string]any{"type": "string", "enum": []string{"udp", "UDP"}},
				"encoding":           map[string]any{"type": "string", "default": "UTF-8"},
				"log_filter_enabled": map[string]any{"type": "boolean"},
				"log_filter_regex":   map[string]any{"type": "string"},
				"source_name":        map[string]any{"type": "string"},
				"data_source_id":     map[string]any{"type": "string"},
				"config_version":     map[string]any{"type": "integer"},
			},
		},
		OutputSchema: plugin.Schema{
			"type": "object",
			"metadata": map[string]any{
				"data_source_id":     "string",
				"data_source_name":   "string",
				"plugin_code":        "syslog",
				"plugin_version":     "string",
				"config_version":     "integer",
				"listener_endpoint":  "string",
				"transport_protocol": "udp",
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
	i.cfg = cfg
	i.status = "initialized"
	i.listener = "stopped"
	i.loadedAt = time.Now().UTC()
	return nil
}

func (i *Input) Start(ctx context.Context, emit plugin.EmitFunc) error {
	i.mu.Lock()
	cfg := i.cfg
	if i.conn != nil {
		i.mu.Unlock()
		return nil
	}
	conn, err := net.ListenPacket(i.cfg.Protocol, i.cfg.Addr)
	if err != nil {
		i.lastErr = err.Error()
		i.status = "failed"
		i.listener = "failed"
		i.mu.Unlock()
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	i.conn = conn
	i.cancel = cancel
	i.status = "running"
	i.listener = "listening"
	i.lastErr = ""
	i.loadedAt = time.Now().UTC()
	i.mu.Unlock()

	go func() {
		<-runCtx.Done()
		_ = i.Stop(context.Background())
	}()

	buf := make([]byte, 65535)
	for {
		n, remote, err := conn.ReadFrom(buf)
		if err != nil {
			if runCtx.Err() != nil {
				return nil
			}
			i.recordError(err)
			continue
		}
		raw := strings.TrimSpace(string(buf[:n]))
		if raw == "" {
			continue
		}
		if cfg.LogFilterEnabled && cfg.LogFilterRegex != nil && !cfg.LogFilterRegex.MatchString(raw) {
			continue
		}
		e := event.New(raw, event.Source{Type: "syslog", Name: cfg.Name, IP: remote.String()}, time.Now().UTC())
		if cfg.Sourcetype != "" {
			e.Metadata["sourcetype"] = cfg.Sourcetype
		}
		e.Metadata["source_name"] = cfg.Name
		e.Metadata["data_source_name"] = cfg.Name
		e.Metadata["data_source_id"] = cfg.DataSourceID
		e.Metadata["plugin_code"] = "syslog"
		e.Metadata["plugin_version"] = cfg.PluginVersion
		e.Metadata["config_version"] = cfg.ConfigVersion
		e.Metadata["listener_endpoint"] = cfg.endpoint()
		e.Metadata["transport_protocol"] = cfg.Protocol
		if err := emit(runCtx, e); err != nil {
			i.recordError(err)
			continue
		}
		i.recordReceived(n)
	}
}

func (i *Input) Stop(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cancel != nil {
		i.cancel()
		i.cancel = nil
	}
	if i.conn != nil {
		_ = i.conn.Close()
		i.conn = nil
	}
	if i.status == "" {
		i.status = "initialized"
	}
	i.listener = "stopped"
	if i.status != "failed" {
		i.status = "stopped"
	}
	return nil
}

func (i *Input) Reload(ctx context.Context, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	wasRunning := false
	i.mu.RLock()
	wasRunning = i.conn != nil
	i.mu.RUnlock()
	if wasRunning {
		_ = i.Stop(ctx)
	}
	i.mu.Lock()
	i.cfg = cfg
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
			"data_source_id":     i.cfg.DataSourceID,
			"data_source_name":   i.cfg.Name,
			"plugin_code":        "syslog",
			"plugin_version":     i.cfg.PluginVersion,
			"config_version":     i.cfg.ConfigVersion,
			"listener_endpoint":  i.cfg.endpoint(),
			"transport_protocol": i.cfg.Protocol,
		},
	}
}

func (i *Input) Close() error { return i.Stop(context.Background()) }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Addr: ":5514", Protocol: "udp", Name: "xdp-agent-syslog", Encoding: "UTF-8", PluginVersion: "1.0.0", ConfigVersion: 1}
	_, hasStandardPort := config["collector_port"]
	if hasStandardPort || config["transport_protocol"] != nil || config["log_filter_enabled"] != nil || config["encoding"] != nil {
		port, ok := intValue(config["collector_port"])
		if !ok || port < 1 || port > 65535 {
			return cfg, fmt.Errorf("collector_port must be between 1 and 65535")
		}
		cfg.Addr = ":" + strconv.Itoa(port)
		protocol := strings.ToLower(strings.TrimSpace(stringValue(config["transport_protocol"], "")))
		if protocol == "" {
			return cfg, fmt.Errorf("transport_protocol is required")
		}
		cfg.Protocol = protocol
		cfg.Encoding = strings.ToUpper(strings.TrimSpace(stringValue(config["encoding"], "")))
		if cfg.Encoding == "" {
			return cfg, fmt.Errorf("encoding is required")
		}
		if _, ok := config["log_filter_enabled"]; !ok {
			return cfg, fmt.Errorf("log_filter_enabled is required")
		}
		cfg.LogFilterEnabled = boolValue(config["log_filter_enabled"])
		if cfg.LogFilterEnabled {
			pattern := strings.TrimSpace(stringValue(config["log_filter_regex"], ""))
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
		cfg.Name = strings.TrimSpace(stringValue(config["source_name"], stringValue(config["name"], cfg.Name)))
		cfg.DataSourceID = strings.TrimSpace(stringValue(config["data_source_id"], ""))
		cfg.PluginVersion = strings.TrimSpace(stringValue(config["plugin_version"], cfg.PluginVersion))
		if v, ok := int64Value(config["config_version"]); ok {
			cfg.ConfigVersion = v
		}
		cfg.Sourcetype = strings.TrimSpace(stringValue(config["sourcetype"], ""))
	} else {
		for key, value := range config {
			text, ok := value.(string)
			if !ok {
				return cfg, fmt.Errorf("%s must be a string", key)
			}
			switch key {
			case "addr":
				cfg.Addr = text
			case "protocol":
				cfg.Protocol = strings.ToLower(text)
			case "name":
				cfg.Name = text
			case "sourcetype":
				cfg.Sourcetype = text
			}
		}
	}
	if cfg.Addr == "" {
		return cfg, fmt.Errorf("addr is required")
	}
	if cfg.Protocol != "udp" {
		return cfg, fmt.Errorf("syslog only supports udp in MVP")
	}
	return cfg, nil
}

func (c Config) endpoint() string {
	port := strings.TrimPrefix(c.Addr, ":")
	if port == "" {
		return ""
	}
	return fmt.Sprintf("%s://0.0.0.0:%s", c.Protocol, port)
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
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		return n, err == nil
	default:
		return 0, false
	}
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
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func stringValue(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		if strings.TrimSpace(text) == "" {
			return fallback
		}
		return text
	}
	return fmt.Sprint(value)
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1" || strings.EqualFold(strings.TrimSpace(typed), "on")
	default:
		return false
	}
}
