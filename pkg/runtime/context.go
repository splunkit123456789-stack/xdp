package runtime

import (
	"context"
	"time"

	"xdp/pkg/event"
)

type ProcessContext struct {
	ctx     context.Context
	event   *event.Event
	stageID string
}

func (c ProcessContext) Context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c ProcessContext) PipelineID() string {
	return c.event.PipelineID
}

func (c ProcessContext) PipelineVersion() string {
	return c.event.PipelineVersion
}

func (c ProcessContext) StageID() string {
	return c.stageID
}

func (c ProcessContext) Attempt() int {
	return 1
}

func (c ProcessContext) Now() time.Time {
	return time.Now().UTC()
}

func (c ProcessContext) AddTag(tag string) {
	c.event.Tags = append(c.event.Tags, tag)
}

func (c ProcessContext) AddError(err event.ProcessingError) {
	c.event.Errors = append(c.event.Errors, err)
}
