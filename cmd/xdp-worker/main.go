package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"xdp/pkg/bus/kafka"
	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
	xdpruntime "xdp/pkg/runtime"
	geoip "xdp/plugins/enrichment/geoip"
	kafkaoutput "xdp/plugins/output/kafka"
	memoryoutput "xdp/plugins/output/memory"
	s3output "xdp/plugins/output/s3"
	jsonparser "xdp/plugins/parser/json"
	propsconfparser "xdp/plugins/parser/propsconf"
	regexparser "xdp/plugins/parser/regex"
	indexrouter "xdp/plugins/router/indexrouter"
	fieldmapping "xdp/plugins/transform/fieldmapping"
	typeconvert "xdp/plugins/transform/typeconvert"
)

func main() {
	ctx := context.Background()
	brokers := strings.Split(env("XDP_KAFKA_BROKERS", "127.0.0.1:9092"), ",")
	configPath := env("XDP_PIPELINE_CONFIG", "configs/pipelines")
	configAPI := strings.TrimRight(env("XDP_CONFIG_API", ""), "/")
	configToken := firstNonEmpty(env("XDP_CONFIG_API_TOKEN", ""), env("XDP_API_TOKEN", ""))
	reloadInterval := durationEnv("XDP_CONFIG_RELOAD_INTERVAL", 5*time.Second)
	bus := kafka.NewKafka(brokers, "xdp-worker")
	reg := newRegistry()
	runner := xdpruntime.NewExecutor(reg)
	outputTopic := kafka.OutputTopic(env("XDP_TARGET", "default"))

	sources, err := loadSourcePipelines(ctx, reg, configPath, configAPI, configToken, true)
	if err != nil {
		slog.Error("build source pipelines failed", "error", err)
		os.Exit(1)
	}
	if len(sources) == 0 {
		slog.Error("no enabled pipelines", "path", configPath)
		os.Exit(1)
	}
	if err := ensureSourceTopics(ctx, bus, sources); err != nil {
		slog.Error("ensure source topics failed", "error", err)
		os.Exit(1)
	}

	signature := sourceSignature(sources)
	slog.Info("xdp-worker started", "output_topic", outputTopic, "pipeline_config", configPath, "config_api", configAPI, "reload_interval", reloadInterval.String(), "loaded_pipelines", len(sources))
	logSources("pipeline source loaded", sources)
	lastReload := time.Now()
	for {
		if configAPI != "" && time.Since(lastReload) >= reloadInterval {
			if next, err := loadSourcePipelines(ctx, reg, configPath, configAPI, configToken, false); err != nil {
				slog.Warn("reload pipeline config failed", "config_api", configAPI, "error", err)
			} else if nextSignature := sourceSignature(next); nextSignature != signature {
				if err := ensureSourceTopics(ctx, bus, next); err != nil {
					slog.Warn("ensure source topics failed", "error", err)
				} else {
					sources = next
					signature = nextSignature
					slog.Info("pipeline config reloaded", "loaded_pipelines", len(sources))
					logSources("pipeline source reloaded", sources)
				}
			}
			lastReload = time.Now()
		}
		for _, source := range sources {
			processSource(ctx, bus, runner, source, outputTopic)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type sourcePipeline struct {
	Topic    string
	Pipeline pipeline.Pipeline
}

type runtimePipelinesResponse struct {
	Pipelines []pipeline.Pipeline `json:"pipelines"`
}

func loadSourcePipelines(ctx context.Context, reg *plugin.Registry, configPath string, configAPI string, configToken string, allowFileFallback bool) ([]sourcePipeline, error) {
	pipes, err := loadPipelines(ctx, configPath, configAPI, configToken, allowFileFallback)
	if err != nil {
		return nil, err
	}
	return buildSourcePipelines(reg, pipes)
}

func loadPipelines(ctx context.Context, configPath string, configAPI string, configToken string, allowFileFallback bool) ([]pipeline.Pipeline, error) {
	if configAPI != "" {
		pipes, err := fetchRuntimePipelines(ctx, configAPI, configToken)
		if err == nil && len(pipes) > 0 {
			return pipes, nil
		}
		if !allowFileFallback {
			if err == nil {
				err = fmt.Errorf("runtime pipeline response is empty")
			}
			return nil, err
		}
		slog.Warn("load pipeline config from api failed, falling back to files", "config_api", configAPI, "error", err)
	}
	return pipeline.LoadPath(configPath)
}

func fetchRuntimePipelines(ctx context.Context, configAPI string, configToken string) ([]pipeline.Pipeline, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, configAPI+"/api/v1/runtime/pipelines", nil)
	if err != nil {
		return nil, err
	}
	if configToken != "" {
		req.Header.Set("Authorization", "Bearer "+configToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("config api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var body runtimePipelinesResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	return body.Pipelines, nil
}

func newRegistry() *plugin.Registry {
	reg := plugin.NewRegistry()
	must(jsonparser.Register(reg))
	must(propsconfparser.Register(reg))
	must(regexparser.Register(reg))
	must(fieldmapping.Register(reg))
	must(typeconvert.Register(reg))
	must(indexrouter.Register(reg))
	must(geoip.Register(reg))
	must(kafkaoutput.Register(reg))
	must(memoryoutput.Register(reg))
	must(s3output.Register(reg))
	return reg
}

func buildSourcePipelines(reg *plugin.Registry, pipes []pipeline.Pipeline) ([]sourcePipeline, error) {
	seen := map[string]string{}
	sources := []sourcePipeline{}
	for _, pipe := range pipes {
		if disabled(pipe.Spec.Source.Enabled) {
			continue
		}
		if err := pipe.Validate(); err != nil {
			return nil, fmt.Errorf("pipeline %s: %w", pipe.Metadata.ID, err)
		}
		if err := validatePipelinePlugins(reg, pipe); err != nil {
			return nil, fmt.Errorf("pipeline %s: %w", pipe.Metadata.ID, err)
		}
		topic, err := rawTopicForSource(pipe.Spec.Source)
		if err != nil {
			return nil, fmt.Errorf("pipeline %s: %w", pipe.Metadata.ID, err)
		}
		if existing, ok := seen[topic]; ok {
			return nil, fmt.Errorf("multiple enabled pipelines map to raw topic %s: %s and %s", topic, existing, pipe.Metadata.ID)
		}
		seen[topic] = pipe.Metadata.ID
		sources = append(sources, sourcePipeline{Topic: topic, Pipeline: pipe})
	}
	return sources, nil
}

func rawTopicForSource(source pipeline.SourceSpec) (string, error) {
	if value, ok := stringConfig(source.Config, "internal_raw_topic"); ok {
		return value, nil
	}
	if value, ok := stringConfig(source.Config, "raw_source"); ok {
		return kafka.RawTopic(value), nil
	}
	name := strings.TrimSuffix(source.Plugin, "-input")
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("source plugin is required to derive raw topic")
	}
	return kafka.RawTopic(name), nil
}

func validatePipelinePlugins(reg *plugin.Registry, pipe pipeline.Pipeline) error {
	for _, stage := range pipe.Spec.Stages {
		if err := validatePipelineStagePlugin(reg, stage); err != nil {
			return err
		}
	}
	for _, output := range pipe.Spec.Outputs {
		if disabled(output.Enabled) {
			continue
		}
		if _, _, err := reg.Get(plugin.TypeOutput, output.Plugin, output.Version); err != nil {
			return fmt.Errorf("output %s references unavailable plugin %s@%s: %w", output.ID, output.Plugin, output.Version, err)
		}
	}
	return nil
}

func validatePipelineStagePlugin(reg *plugin.Registry, stage pipeline.StageSpec) error {
	if disabled(stage.Enabled) {
		return nil
	}
	if stage.Type == "parser_group" {
		for _, child := range stage.Stages {
			if err := validatePipelineStagePlugin(reg, child); err != nil {
				return err
			}
		}
		return nil
	}
	if _, _, err := reg.Get(plugin.Type(stage.Type), stage.Plugin, stage.Version); err != nil {
		return fmt.Errorf("stage %s references unavailable plugin %s/%s@%s: %w", stage.ID, stage.Type, stage.Plugin, stage.Version, err)
	}
	return nil
}

func stringConfig(config map[string]any, key string) (string, bool) {
	if config == nil {
		return "", false
	}
	value, ok := config[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

func disabled(enabled *bool) bool {
	return enabled != nil && !*enabled
}

func sourceSignature(sources []sourcePipeline) string {
	payload, _ := json.Marshal(sources)
	return string(payload)
}

type topicEnsurer interface {
	EnsureTopic(ctx context.Context, topic string) error
}

func ensureSourceTopics(ctx context.Context, ensurer topicEnsurer, sources []sourcePipeline) error {
	for _, source := range sources {
		if err := ensurer.EnsureTopic(ctx, source.Topic); err != nil {
			return fmt.Errorf("ensure topic %s: %w", source.Topic, err)
		}
	}
	return nil
}

func logSources(message string, sources []sourcePipeline) {
	for _, source := range sources {
		slog.Info(message, "topic", source.Topic, "pipeline_id", source.Pipeline.Metadata.ID)
	}
}

func processSource(ctx context.Context, bus *kafka.Kafka, runner *xdpruntime.Executor, source sourcePipeline, outputTopic string) {
	messages, err := bus.Consume(ctx, source.Topic, 10)
	if err != nil {
		if !strings.Contains(err.Error(), "context deadline") {
			slog.Warn("consume failed", "topic", source.Topic, "error", err)
		}
		return
	}
	deadletterTopic := kafka.DeadletterTopic(source.Pipeline.Metadata.ID)
	for _, msg := range messages {
		var e event.Event
		if err := json.Unmarshal(msg.Value, &e); err != nil {
			slog.Warn("drop invalid event payload", "topic", source.Topic, "error", err)
			continue
		}
		result, err := runner.Execute(ctx, source.Pipeline, &e)
		payload, _ := json.Marshal(result.Event)
		if err != nil {
			slog.Warn("pipeline execution failed", "pipeline_id", source.Pipeline.Metadata.ID, "topic", source.Topic, "event_id", e.EventID, "error", err)
			if produceErr := bus.Produce(ctx, kafka.Message{Topic: deadletterTopic, Key: e.EventID, Value: payload}); produceErr != nil {
				slog.Warn("produce deadletter failed", "topic", deadletterTopic, "event_id", e.EventID, "error", produceErr)
			}
			continue
		}
		if err := bus.Produce(ctx, kafka.Message{Topic: outputTopic, Key: e.EventID, Value: payload}); err != nil {
			slog.Warn("produce output failed", "topic", outputTopic, "event_id", e.EventID, "error", err)
			continue
		}
		slog.Info("event processed", "pipeline_id", source.Pipeline.Metadata.ID, "input_topic", source.Topic, "output_topic", outputTopic, "event_id", e.EventID)
	}
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

func must(err error) {
	if err != nil {
		panic(err)
	}
}
