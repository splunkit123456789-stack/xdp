package pipeline

import "testing"

func TestLoadDirLoadsConfiguredPipelines(t *testing.T) {
	pipes, err := LoadDir("../../configs/pipelines")
	if err != nil {
		t.Fatalf("LoadDir() error = %v", err)
	}
	if len(pipes) != 2 {
		t.Fatalf("LoadDir() loaded %d pipelines, want 2", len(pipes))
	}
	ids := map[string]bool{}
	for _, pipe := range pipes {
		ids[pipe.Metadata.ID] = true
	}
	if !ids[JSONPipelineID] {
		t.Fatalf("LoadDir() missing %s", JSONPipelineID)
	}
	if !ids[FirewallPipelineID] {
		t.Fatalf("LoadDir() missing %s", FirewallPipelineID)
	}
}
