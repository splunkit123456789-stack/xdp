package pipeline

const SyslogCollectionPipelineID = "syslog-collection-pipeline"

func SyslogCollectionPipeline() Pipeline {
	return Pipeline{
		APIVersion: APIVersionV1Alpha1,
		Kind:       KindPipeline,
		Metadata:   Metadata{ID: SyslogCollectionPipelineID, Name: "Syslog Collection Pipeline"},
		Spec: Spec{
			Source:  SourceSpec{ID: "raw-syslog", Type: "input", Plugin: "syslog", Version: "1.0.0"},
			Stages:  []StageSpec{},
			Outputs: []OutputSpec{{ID: "memory-search", Plugin: "memory-output", Version: "1.0.0", Config: map[string]any{"index": "${metadata.index}"}}},
		},
	}
}
