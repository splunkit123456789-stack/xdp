package pipeline

import "testing"

func TestLoadDirLoadsConfiguredPipelines(t *testing.T) {
	pipes, err := LoadDir("../../configs/pipelines")
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(pipes) != 1 {
		t.Fatalf("LoadDir() loaded %d pipelines, want 1", len(pipes))
	}
	ids := map[string]bool{}
	for _, pipe := range pipes {
		ids[pipe.Metadata.ID] = true
	}
	if !ids[SyslogCollectionPipelineID] {
		t.Fatalf("LoadDir() missing %s", SyslogCollectionPipelineID)
	}
}
