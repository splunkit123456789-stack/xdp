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
			name:   "syslog input plugin",
			source: pipeline.SourceSpec{Plugin: "syslog"},
			want:   "xdp.raw.syslog",
		},
		{
			name:   "raw source override",
			source: pipeline.SourceSpec{Plugin: "syslog", Config: map[string]any{"raw_source": "custom"}},
			want:   "xdp.raw.custom",
		},
		{
			name:   "internal raw topic override",
			source: pipeline.SourceSpec{Plugin: "syslog", Config: map[string]any{"internal_raw_topic": "raw.ds_explicit"}},
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
		pipeline.SyslogCollectionPipeline(),
		pipeline.SyslogCollectionPipeline(),
	}
	pipes[1].Metadata.ID = "duplicate-syslog-pipeline"

	_, err := buildSourcePipelines(newRegistry(), pipes)
	if err == nil {
		t.Fatal("buildSourcePipelines() expected duplicate topic error")
	}
}

func TestBuildSourcePipelinesAcceptsParserGroupAndValidatesChildren(t *testing.T) {
	pipe := pipeline.SyslogCollectionPipeline()
	pipe.Spec.Stages = []pipeline.StageSpec{{
		ID:   "parse-rule-group",
		Type: "parser_group",
		Stages: []pipeline.StageSpec{{
			ID:      "parse-rule-regex",
			Type:    "parser",
			Plugin:  "regex",
			Version: "1.0.0",
			Config:  map[string]any{"regex_pattern": "src=(?<src_ip>\\S+)"},
		}},
	}}

	if _, err := buildSourcePipelines(newRegistry(), []pipeline.Pipeline{pipe}); err != nil {
		t.Fatalf("buildSourcePipelines() error = %v", err)
	}
}

func TestEnsureSourceTopicsCreatesEachRawTopic(t *testing.T) {
	ensurer := &recordingTopicEnsurer{}
	sources := []sourcePipeline{
		{Topic: "xdp.raw.syslog"},
		{Topic: "raw.ds_custom_syslog"},
	}

	if err := ensureSourceTopics(context.Background(), ensurer, sources); err != nil {
		t.Fatalf("ensureSourceTopics() error = %v", err)
	}

	want := []string{"xdp.raw.syslog", "raw.ds_custom_syslog"}
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
