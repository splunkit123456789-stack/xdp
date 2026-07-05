package s3

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xdp/pkg/plugin"
)

type Output struct{ dir string }

func New() *Output { return &Output{} }
func (o *Output) Metadata() plugin.Metadata {
	return plugin.Metadata{Code: "s3-output", Name: "S3/MinIO Archive Output", Type: plugin.TypeOutput, Version: "1.0.0", Runtime: "go", ConfigSchema: plugin.Schema{"type": "object"}}
}
func (o *Output) Validate(config map[string]any) error { return nil }
func (o *Output) Init(ctx plugin.InitContext, config map[string]any) error {
	o.dir, _ = config["local_dir"].(string)
	if o.dir == "" {
		o.dir = "data/minio-archive"
	}
	return nil
}
func (o *Output) Close() error { return nil }
func (o *Output) Write(ctx context.Context, batch *plugin.EventBatch) error {
	if err := os.MkdirAll(o.dir, 0755); err != nil {
		return err
	}
	name := filepath.Join(o.dir, fmt.Sprintf("events-%s.jsonl.gz", time.Now().UTC().Format("20060102150405")))
	file, err := os.Create(name)
	if err != nil {
		return err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	defer gz.Close()
	enc := json.NewEncoder(gz)
	for _, e := range batch.Events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}
func Register(reg *plugin.Registry) error {
	item := New()
	return reg.Register(item.Metadata(), func() any { return New() })
}
func safe(value string) string { return strings.NewReplacer("/", "_", "..", "_").Replace(value) }
