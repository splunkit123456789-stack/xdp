package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/eventtime"
	"xdp/pkg/plugin"
	sysloginput "xdp/plugins/input/syslog"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	brokers := strings.Split(env("XDP_KAFKA_BROKERS", "127.0.0.1:9092"), ",")
	producer := kafka.NewKafka(brokers, "xdp-agent")
	configAPI := strings.TrimRight(env("XDP_CONFIG_API", ""), "/")
	configToken := firstNonEmpty(env("XDP_CONFIG_API_TOKEN", ""), env("XDP_API_TOKEN", ""))
	reloadInterval := durationEnv("XDP_CONFIG_RELOAD_INTERVAL", 5*time.Second)
	router := newTopicRouter()
	reg := plugin.NewRegistry()
	must(sysloginput.Register(reg))
	listeners := newSyslogListenerManager(reg, producer)
	if configAPI != "" {
		go router.reloadLoop(ctx, configAPI, configToken, reloadInterval)
		go listeners.reconcileLoop(ctx, router, reloadInterval)
	} else {
		runInput(ctx, reg, plugin.TypeInput, "syslog", "1.0.0", map[string]any{
			"addr":     env("XDP_SYSLOG_ADDR", ":5514"),
			"protocol": "udp",
			"name":     "xdp-agent-syslog",
		}, router.emitToKafka(producer, "syslog"), false)
	}

	runAgentHTTPServer(ctx, env("XDP_AGENT_ADDR", ":8081"), router.emitToKafka(producer, "http"))

	<-ctx.Done()
}

func runInput(ctx context.Context, reg *plugin.Registry, pluginType plugin.Type, code string, version string, config map[string]any, emit plugin.EmitFunc, required bool) {
	factory, _, err := reg.Get(pluginType, code, version)
	if err != nil {
		slog.Error("input plugin not found", "plugin", code, "error", err)
		os.Exit(1)
	}
	input, ok := factory().(plugin.InputPlugin)
	if !ok {
		slog.Error("plugin is not an input", "plugin", code)
		os.Exit(1)
	}
	if err := input.Init(plugin.BasicInitContext{Ctx: ctx, Code: code, Version: version}, config); err != nil {
		slog.Error("input plugin init failed", "plugin", code, "error", err)
		os.Exit(1)
	}
	go func() {
		slog.Info("input plugin started", "plugin", code)
		if err := input.Start(ctx, emit); err != nil {
			if required {
				slog.Error("input plugin stopped", "plugin", code, "error", err)
				os.Exit(1)
			}
			slog.Warn("input plugin disabled", "plugin", code, "error", err)
		}
	}()
}

func runAgentHTTPServer(ctx context.Context, addr string, emit plugin.EmitFunc) {
	server := &http.Server{Addr: addr, Handler: newAgentManagementHandlerWithIngest(emit), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		slog.Info("agent http server started", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("agent http server stopped", "error", err)
			os.Exit(1)
		}
	}()
}

func newAgentManagementHandler() http.Handler {
	return newAgentManagementHandlerWithIngest(nil)
}

func newAgentManagementHandlerWithIngest(emit plugin.EmitFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		if emit == nil {
			writeAgentError(w, http.StatusServiceUnavailable, "INGEST_UNAVAILABLE", "ingest is unavailable")
			return
		}
		defer r.Body.Close()
		var req struct {
			Raw            string `json:"raw"`
			TimeField      string `json:"time_field"`
			EventTimeField string `json:"event_time_field"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Raw) == "" {
			writeAgentError(w, http.StatusBadRequest, "INVALID_REQUEST", "raw is required")
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
				writeAgentError(w, http.StatusBadRequest, "INVALID_TIME_FIELD", "invalid time_field")
				return
			}
			eventTime = parsed
		}
		e := event.New(req.Raw, event.Source{Type: "http", Name: "xdp-agent"}, now)
		e.EventTime = eventTime
		if timeField != "" {
			e.Metadata["time_field"] = timeField
		}
		if err := emit(r.Context(), e); err != nil {
			writeAgentError(w, http.StatusBadGateway, "EMIT_FAILED", err.Error())
			return
		}
		writeAgentJSON(w, http.StatusOK, map[string]any{"status": "queued", "event_id": e.EventID})
	})
	mux.HandleFunc("POST /api/v1/port-check", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req struct {
			PluginCode        string `json:"plugin_code"`
			TransportProtocol string `json:"transport_protocol"`
			CollectorPort     int    `json:"collector_port"`
			ListenHost        string `json:"listen_host,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAgentError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid port check request")
			return
		}
		if strings.ToLower(strings.TrimSpace(req.PluginCode)) != "syslog" {
			writeAgentError(w, http.StatusBadRequest, "VALIDATION_ERROR", "plugin_code must be syslog in P0")
			return
		}
		if req.CollectorPort < 1 || req.CollectorPort > 65535 {
			writeAgentError(w, http.StatusBadRequest, "VALIDATION_ERROR", "collector_port must be between 1 and 65535")
			return
		}
		protocol := strings.ToUpper(strings.TrimSpace(req.TransportProtocol))
		if protocol != "UDP" {
			writeAgentError(w, http.StatusBadRequest, "VALIDATION_ERROR", "transport_protocol must be UDP in P0")
			return
		}
		host := strings.TrimSpace(req.ListenHost)
		if host == "" {
			host = "0.0.0.0"
		}
		if err := probeAgentListenerPort(host, protocol, req.CollectorPort); err != nil {
			writeAgentError(w, http.StatusConflict, "LISTENER_PORT_UNAVAILABLE", "端口不可用")
			return
		}
		writeAgentJSON(w, http.StatusOK, map[string]any{
			"available":          true,
			"collector_port":     req.CollectorPort,
			"listen_host":        host,
			"transport_protocol": protocol,
		})
	})
	return mux
}

func probeAgentListenerPort(host string, protocol string, port int) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	if strings.EqualFold(protocol, "UDP") {
		conn, err := net.ListenPacket("udp", address)
		if err != nil {
			return err
		}
		return conn.Close()
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	return ln.Close()
}

func writeAgentJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAgentError(w http.ResponseWriter, status int, code string, message string) {
	writeAgentJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}})
}

type syslogListenerManager struct {
	reg      *plugin.Registry
	producer *kafka.Kafka
	mu       sync.Mutex
	running  map[string]runningListener
}

type runningListener struct {
	spec   syslogSpec
	cancel context.CancelFunc
}

func newSyslogListenerManager(reg *plugin.Registry, producer *kafka.Kafka) *syslogListenerManager {
	return &syslogListenerManager{reg: reg, producer: producer, running: map[string]runningListener{}}
}

func (m *syslogListenerManager) reconcileLoop(ctx context.Context, router *topicRouter, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		m.reconcile(ctx, router.syslogSpecs())
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-ticker.C:
		}
	}
}

func (m *syslogListenerManager) reconcile(ctx context.Context, desired map[string]syslogSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, current := range m.running {
		next, ok := desired[id]
		if !ok || next != current.spec {
			current.cancel()
			delete(m.running, id)
		}
	}
	for id, spec := range desired {
		if _, ok := m.running[id]; ok {
			continue
		}
		listenerCtx, cancel := context.WithCancel(ctx)
		m.running[id] = runningListener{spec: spec, cancel: cancel}
		go m.run(listenerCtx, spec)
	}
}

func (m *syslogListenerManager) run(ctx context.Context, spec syslogSpec) {
	factory, _, err := m.reg.Get(plugin.TypeInput, "syslog", "1.0.0")
	if err != nil {
		slog.Warn("syslog listener plugin unavailable", "datasource", spec.ID, "error", err)
		return
	}
	input, ok := factory().(plugin.InputPlugin)
	if !ok {
		slog.Warn("syslog listener factory returned non-input", "datasource", spec.ID)
		return
	}
	if err := input.Init(plugin.BasicInitContext{Ctx: ctx, Code: "syslog", Version: "1.0.0"}, map[string]any{
		"addr":     spec.Addr,
		"protocol": spec.Protocol,
		"name":     spec.Name,
	}); err != nil {
		slog.Warn("syslog listener init failed", "datasource", spec.ID, "error", err)
		return
	}
	emit := func(ctx context.Context, e *event.Event) error {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		return m.producer.Produce(ctx, kafka.Message{Topic: spec.Topic, Key: e.EventID, Value: data})
	}
	slog.Info("syslog listener started", "datasource", spec.ID, "addr", spec.Addr, "topic", spec.Topic)
	if err := input.Start(ctx, emit); err != nil {
		slog.Warn("syslog listener stopped", "datasource", spec.ID, "error", err)
	}
}

func (m *syslogListenerManager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, current := range m.running {
		current.cancel()
		delete(m.running, id)
	}
}

type topicRouter struct {
	mu     sync.RWMutex
	topics map[string]string
	syslog map[string]syslogSpec
}

type dataSourcesResponse struct {
	DataSources []agentDataSource `json:"datasources"`
}

type agentDataSource struct {
	ID               string         `json:"id"`
	Type             string         `json:"type"`
	Name             string         `json:"name"`
	Status           string         `json:"status"`
	InternalRawTopic string         `json:"internal_raw_topic"`
	PluginCode       string         `json:"plugin_code"`
	PluginConfig     map[string]any `json:"plugin_config"`
}

func newTopicRouter() *topicRouter {
	return &topicRouter{topics: map[string]string{}, syslog: map[string]syslogSpec{}}
}

func (r *topicRouter) topic(source string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if topic := strings.TrimSpace(r.topics[source]); topic != "" {
		return topic
	}
	return kafka.RawTopic(source)
}

func (r *topicRouter) emitToKafka(producer *kafka.Kafka, source string) plugin.EmitFunc {
	return func(ctx context.Context, e *event.Event) error {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		topic := r.topic(source)
		if err := producer.Produce(ctx, kafka.Message{Topic: topic, Key: e.EventID, Value: data}); err != nil {
			return err
		}
		slog.Info("event queued", "topic", topic, "event_id", e.EventID)
		return nil
	}
}

type syslogSpec struct {
	ID       string
	Name     string
	Addr     string
	Protocol string
	Topic    string
}

func (r *topicRouter) syslogSpecs() map[string]syslogSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]syslogSpec, len(r.syslog))
	for id, spec := range r.syslog {
		out[id] = spec
	}
	return out
}

func (r *topicRouter) reloadLoop(ctx context.Context, configAPI string, configToken string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := r.reload(ctx, configAPI, configToken); err != nil {
			slog.Warn("reload datasource topics failed", "config_api", configAPI, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *topicRouter) reload(ctx context.Context, configAPI string, configToken string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, configAPI+"/api/v1/datasources", nil)
	if err != nil {
		return err
	}
	if configToken != "" {
		req.Header.Set("Authorization", "Bearer "+configToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return errStatus(resp.StatusCode, data)
	}
	var body dataSourcesResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return err
	}
	next := map[string]string{}
	nextSyslog := map[string]syslogSpec{}
	for _, source := range body.DataSources {
		if source.Status != "" && source.Status != "active" {
			continue
		}
		topic := strings.TrimSpace(source.InternalRawTopic)
		if topic == "" {
			continue
		}
		next[source.Type] = topic
		if source.collectPluginCode() == "syslog" {
			spec := source.syslogSpec(topic)
			if spec.ID != "" {
				nextSyslog[spec.ID] = spec
			}
		}
	}
	r.mu.Lock()
	r.topics = next
	r.syslog = nextSyslog
	r.mu.Unlock()
	return nil
}

func (s agentDataSource) collectPluginCode() string {
	if strings.TrimSpace(s.PluginCode) != "" {
		return strings.ToLower(strings.TrimSpace(s.PluginCode))
	}
	return strings.ToLower(strings.TrimSpace(s.Type))
}

func (s agentDataSource) syslogSpec(topic string) syslogSpec {
	id := strings.TrimSpace(s.ID)
	if id == "" {
		id = strings.TrimSpace(s.Name)
	}
	if id == "" {
		return syslogSpec{}
	}
	port := intFromConfig(s.PluginConfig, "collector_port", 5514)
	protocol := strings.ToLower(stringFromConfig(s.PluginConfig, "transport_protocol", "udp"))
	if protocol == "" {
		protocol = "udp"
	}
	name := strings.TrimSpace(s.Name)
	if name == "" {
		name = id
	}
	return syslogSpec{
		ID:       id,
		Name:     name,
		Addr:     fmt.Sprintf(":%d", port),
		Protocol: protocol,
		Topic:    topic,
	}
}

func intFromConfig(config map[string]any, key string, fallback int) int {
	if config == nil {
		return fallback
	}
	switch value := config[key].(type) {
	case int:
		if value > 0 {
			return value
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case float64:
		if value > 0 {
			return int(value)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func stringFromConfig(config map[string]any, key string, fallback string) string {
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

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func errStatus(status int, body []byte) error {
	return &statusError{status: status, body: strings.TrimSpace(string(body))}
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("config api returned %d: %s", e.status, e.body)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
