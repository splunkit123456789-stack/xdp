package plugin

import "context"

type BasicInitContext struct {
	Ctx     context.Context
	Code    string
	Version string
}

func (c BasicInitContext) Context() context.Context {
	if c.Ctx == nil {
		return context.Background()
	}
	return c.Ctx
}

func (c BasicInitContext) PluginCode() string {
	return c.Code
}

func (c BasicInitContext) PluginVersion() string {
	return c.Version
}
