package pipeline

import "fmt"

const APIVersionV1Alpha1 = "xdp.io/v1alpha1"
const KindPipeline = "Pipeline"

func (p Pipeline) Validate() error {
	if p.APIVersion != APIVersionV1Alpha1 {
		return fmt.Errorf("unsupported api_version: %s", p.APIVersion)
	}
	if p.Kind != KindPipeline {
		return fmt.Errorf("unsupported kind: %s", p.Kind)
	}
	if p.Metadata.ID == "" {
		return fmt.Errorf("metadata.id is required")
	}
	if p.Spec.Source.Plugin == "" {
		return fmt.Errorf("spec.source.plugin is required")
	}
	if len(p.Spec.Stages) == 0 {
		return fmt.Errorf("spec.stages must not be empty")
	}
	if len(p.Spec.Outputs) == 0 {
		return fmt.Errorf("spec.outputs must not be empty")
	}
	ids := map[string]struct{}{}
	for _, stage := range p.Spec.Stages {
		if stage.ID == "" {
			return fmt.Errorf("stage id is required")
		}
		if _, exists := ids[stage.ID]; exists {
			return fmt.Errorf("duplicate stage id: %s", stage.ID)
		}
		ids[stage.ID] = struct{}{}
	}
	for _, output := range p.Spec.Outputs {
		if output.ID == "" {
			return fmt.Errorf("output id is required")
		}
		if _, exists := ids[output.ID]; exists {
			return fmt.Errorf("duplicate output id: %s", output.ID)
		}
		ids[output.ID] = struct{}{}
	}
	return nil
}
