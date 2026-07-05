package main

import (
	"context"
	"testing"

	"xdp/pkg/pipeline"
)

func TestRawTopicForSource(t *testing.T) {
	tests := []struct {
		name   string
		source pipeline.SourceSpec
		want   string
	}{
		{
			name:   "http input plugin",
			source: pipeline.SourceSpec{Plugin: "http-input"},
			want:   "xdp.raw.http",
		},
		{
			name:   "syslog input plugin",
			source: pipeline.SourceSpec{Plugin: "syslog-input"},
			want:   "xdp.raw.syslog",
		},
		{
			name:   "raw source override",
			source: pipeline.SourceSpec{Plugin: "http-input", Config: map[string]any{"raw_source": "custom"}},
			want:   "xdp.raw.custom",
		},
		{
			name:   "internal raw topic override",
			source: pipeline.SourceSpec{Plugin: "http-input", Config: map[string]any{"internal_raw_topic": "raw.ds_explicit"}},
			want:   "raw.ds_explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := rawTopicForSource(tt.source)
			if err != nil {
				t.Fatalf("rawTopicForSource() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("rawTopicForSource() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSourcePipelinesRejectsDuplicateTopic(t *testing.T) {
	pipes := []pipeline.Pipeline{
		pipeline.MVPJSONPipeline(),
		pipeline.MVPJSONPipeline(),
	}
	pipes[1].Metadata.ID = "duplicate-json-pipeline"

	_, err := buildSourcePipelines(newRegistry(), pipes)
	if err == nil {
		t.Fatal("buildSourcePipelines() expected duplicate topic error")
	}
}

func TestBuildSourcePipelinesAcceptsParserGroupAndValidatesChildren(t *testing.T) {
	pipe := pipeline.MVPJSONPipeline()
	pipe.Spec.Stages = []pipeline.StageSpec{{
		ID:   "parse-rule-group",
		Type: "parser_group",
		Stages: []pipeline.StageSpec{{
			ID:      "parse-rule-json",
			Type:    "parser",
			Plugin:  "json-parser",
			Version: "1.0.0",
			Config:  map[string]any{"source": "raw", "target": "fields"},
		}},
	}}

	if _, err := buildSourcePipelines(newRegistry(), []pipeline.Pipeline{pipe}); err != nil {
		t.Fatalf("buildSourcePipelines() error = %v", err)
	}
}

func TestEnsureSourceTopicsCreatesEachRawTopic(t *testing.T) {
	ensurer := &recordingTopicEnsurer{}
	sources := []sourcePipeline{
		{Topic: "xdp.raw.http"},
		{Topic: "raw.ds_custom_syslog"},
	}

	if err := ensureSourceTopics(context.Background(), ensurer, sources); err != nil {
		t.Fatalf("ensureSourceTopics() error = %v", err)
	}

	want := []string{"xdp.raw.http", "raw.ds_custom_syslog"}
	if len(ensurer.topics) != len(want) {
		t.Fatalf("ensured topics = %#v, want %#v", ensurer.topics, want)
	}
	for i, topic := range want {
		if ensurer.topics[i] != topic {
			t.Fatalf("ensured topic[%d] = %q, want %q", i, ensurer.topics[i], topic)
		}
	}
}

type recordingTopicEnsurer struct {
	topics []string
}

func (r *recordingTopicEnsurer) EnsureTopic(ctx context.Context, topic string) error {
	r.topics = append(r.topics, topic)
	return nil
}
