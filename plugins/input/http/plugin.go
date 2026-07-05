package http

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strings"
	"time"

	"xdp/pkg/event"
	"xdp/pkg/eventtime"
	"xdp/pkg/plugin"
)

type Input struct {
	cfg Config
}

type Config struct {
	Addr   string
	Path   string
	Method string
	Name   string
}

func New() *Input { return &Input{} }

func (i *Input) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Code:        "http-input",
		Name:        "HTTP Input",
		Type:        plugin.TypeInput,
		Version:     "1.0.0",
		Description: "Receive raw events through HTTP POST",
		Runtime:     "go",
		ConfigSchema: plugin.Schema{
			"type": "object",
			"properties": map[string]any{
				"addr":   map[string]any{"type": "string", "default": ":8081"},
				"path":   map[string]any{"type": "string", "default": "/ingest"},
				"method": map[string]any{"type": "string", "default": "POST"},
				"name":   map[string]any{"type": "string", "default": "xdp-agent"},
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
	mux := nethttp.NewServeMux()
	mux.HandleFunc(i.cfg.Method+" "+i.cfg.Path, func(w nethttp.ResponseWriter, r *nethttp.Request) {
		defer r.Body.Close()
		var req struct {
			Raw            string `json:"raw"`
			TimeField      string `json:"time_field"`
			EventTimeField string `json:"event_time_field"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Raw) == "" {
			nethttp.Error(w, "raw is required", nethttp.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		eventTime := now
		timeField := strings.TrimSpace(req.TimeField)
		if timeField == "" {
			timeField = strings.TrimSpace(req.EventTimeField)
		}
		if timeField != "" {
			parsed, err := eventtime.FromRaw(req.Raw, timeField)
			if err != nil {
				nethttp.Error(w, "invalid time_field", nethttp.StatusBadRequest)
				return
			}
			eventTime = parsed
		}
		e := event.New(req.Raw, event.Source{Type: "http", Name: i.cfg.Name}, now)
		e.EventTime = eventTime
		if timeField != "" {
			e.Metadata["time_field"] = timeField
		}
		if err := emit(r.Context(), e); err != nil {
			nethttp.Error(w, err.Error(), nethttp.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "queued", "event_id": e.EventID})
	})
	mux.HandleFunc("GET /healthz", func(w nethttp.ResponseWriter, r *nethttp.Request) { _, _ = w.Write([]byte("ok")) })

	server := &nethttp.Server{Addr: i.cfg.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && err != nethttp.ErrServerClosed {
		return err
	}
	return nil
}

func (i *Input) Close() error { return nil }

func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}

func parseConfig(config map[string]any) (Config, error) {
	cfg := Config{Addr: ":8081", Path: "/ingest", Method: nethttp.MethodPost, Name: "xdp-agent"}
	for key, value := range config {
		text, ok := value.(string)
		if !ok {
			return cfg, fmt.Errorf("%s must be a string", key)
		}
		switch key {
		case "addr":
			cfg.Addr = text
		case "path":
			cfg.Path = text
		case "method":
			cfg.Method = strings.ToUpper(text)
		case "name":
			cfg.Name = text
		}
	}
	if cfg.Addr == "" {
		return cfg, fmt.Errorf("addr is required")
	}
	if cfg.Path == "" || !strings.HasPrefix(cfg.Path, "/") {
		return cfg, fmt.Errorf("path must start with /")
	}
	if cfg.Method == "" {
		return cfg, fmt.Errorf("method is required")
	}
	return cfg, nil
}
