package syslog

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/plugin"
)

type Input struct {
	cfg Config
}

type Config struct {
	Addr       string
	Protocol   string
	Name       string
	Sourcetype string
}

func New() *Input { return &Input{} }

func (i *Input) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "syslog-input",
		Name:        "Syslog Input",
		Type:        plugin.TypeInput,
		Version:     "1.0.0",
		Description: "Receive raw syslog events over UDP",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"addr":       map[string]any{"type": "string", "default": ":5514"},
				"protocol":   map[string]any{"type": "string", "default": "udp"},
				"name":       map[string]any{"type": "string", "default": "xdp-agent-syslog"},
				"sourcetype": map[string]any{"type": "string", "default": ""},
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
	i.cfg = cfg
	return nil
}

func (i *Input) Start(ctx context.Context, emit plugin.EmitFunc) error {
	conn, err := net.ListenPacket(i.cfg.Protocol, i.cfg.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	buf := make([]byte, 65535)
	for {
		n, remote, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		raw := strings.TrimSpace(string(buf[:n]))
		if raw == "" {
			continue
		}
		e := event.New(raw, event.Source{Type: "syslog", Name: i.cfg.Name, IP: remote.String()}, time.Now().UTC())
		if i.cfg.Sourcetype != "" {
			e.Metadata["sourcetype"] = i.cfg.Sourcetype
		}
		if err := emit(ctx, e); err != nil {
			continue
		}
	}
}

func (i *Input) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Addr: ":5514", Protocol: "udp", Name: "xdp-agent-syslog"}
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
	if cfg.Addr == "" {
		return cfg, fmt.Errorf("addr is required")
	}
	if cfg.Protocol != "udp" {
		return cfg, fmt.Errorf("syslog-input only supports udp in MVP")
	}
	return cfg, nil
}
