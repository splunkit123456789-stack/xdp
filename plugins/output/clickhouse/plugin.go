package clickhouse

import (
	"context"
	"fmt"

	"xdp/pkg/plugin"
	ch "xdp/pkg/storage/clickhouse"
)

type Output struct {
	client *ch.Client
	cfg    Config
}

type Config struct {
	Endpoint string
	Database string
	Username string
	Password string
	Index    string
}

func New() *Output {
	return &Output{}
}

func (o *Output) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "clickhouse-output",
		Name:        "ClickHouse Output",
		Type:        plugin.TypeOutput,
		Version:     "1.0.0",
		Description: "Write events to per-index ClickHouse tables",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"endpoint": map[string]any{"type": "string", "default": "http://127.0.0.1:8123"},
				"database": map[string]any{"type": "string", "default": "xdp"},
				"index":    map[string]any{"type": "string", "default": "${metadata.index}"},
			},
		},
	}
}

func (o *Output) Validate(config map[string]any) error {
	_, err := parseConfig(config)
	return err
}

func (o *Output) Init(ctx plugin.InitContext, config map[string]any) error {
	cfg, err := parseConfig(config)
	if err != nil {
		return err
	}
	o.cfg = cfg
	o.client = ch.New(ch.Config{Endpoint: cfg.Endpoint, Database: cfg.Database, Username: cfg.Username, Password: cfg.Password})
	return nil
}

func (o *Output) Write(ctx context.Context, batch *plugin.EventBatch) error {
	if o.client == nil {
		return plugin.NewError(plugin.ErrInvalidConfig, "clickhouse output is not initialized", false, nil)
	}
	for _, e := range batch.Events {
		if o.cfg.Index != "" && o.cfg.Index != "${metadata.index}" {
			e.Metadata["index"] = o.cfg.Index
		}
	}
	if err := o.client.InsertEvents(ctx, batch.Events); err != nil {
		return plugin.NewError(plugin.ErrOutputFailed, "clickhouse insert failed", true, err)
	}
	return nil
}

func (o *Output) Close() error {
	return nil
}

func Register(reg *plugin.Registry) error {
	output := New()
	return reg.Register(output.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Endpoint: "http://127.0.0.1:8123", Database: "xdp", Index: "${metadata.index}"}
	for key, value := range config {
		text, ok := value.(string)
		if !ok {
			return cfg, fmt.Errorf("%s must be a string", key)
		}
		switch key {
		case "endpoint":
			cfg.Endpoint = text
		case "database":
			cfg.Database = text
		case "username":
			cfg.Username = text
		case "password":
			cfg.Password = text
		case "index":
			cfg.Index = text
		}
	}
	return cfg, nil
}
