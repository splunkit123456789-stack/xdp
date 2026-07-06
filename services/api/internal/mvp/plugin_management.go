package mvp

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"xdp/pkg/plugin"
	mysqlstore "xdp/pkg/storage/mysql"
)

type pluginManifest struct {
	PluginCode         string         `json:"plugin_code"`
	PluginType         string         `json:"plugin_type"`
	Version            string         `json:"version"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Runtime            string         `json:"runtime"`
	Entrypoint         string         `json:"entrypoint"`
	MinPlatformVersion string         `json:"min_platform_version"`
	ConfigSchema       map[string]any `json:"config_schema"`
	UISchema           map[string]any `json:"ui_schema"`
	InputSchema        map[string]any `json:"input_schema"`
	OutputSchema       map[string]any `json:"output_schema"`
	PermissionSchema   map[string]any `json:"permission_schema"`
	Capabilities       map[string]any `json:"capabilities"`
}

type PluginImportResponse struct {
	PluginCode       string         `json:"plugin_code"`
	PluginType       string         `json:"plugin_type"`
	PluginVersion    string         `json:"plugin_version"`
	Name             string         `json:"name"`
	Description      string         `json:"description,omitempty"`
	Runtime          string         `json:"runtime"`
	Entrypoint       string         `json:"entrypoint,omitempty"`
	Status           string         `json:"status"`
	Checksum         string         `json:"checksum"`
	ConfigSchema     map[string]any `json:"config_schema,omitempty"`
	UISchema         map[string]any `json:"ui_schema,omitempty"`
	InputSchema      map[string]any `json:"input_schema,omitempty"`
	OutputSchema     map[string]any `json:"output_schema,omitempty"`
	PermissionSchema map[string]any `json:"permission_schema,omitempty"`
}

func readPluginPackage(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, fmt.Errorf("invalid multipart plugin package")
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("plugin file is required")
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("read plugin file failed")
		}
		return data, nil
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read plugin package failed")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("plugin package is empty")
	}
	return data, nil
}

func parsePluginManifest(data []byte) (pluginManifest, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return pluginManifest{}, fmt.Errorf("plugin package must be a zip file")
	}
	for _, file := range zr.File {
		if file.Name != "manifest.json" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return pluginManifest{}, fmt.Errorf("open manifest failed")
		}
		defer rc.Close()
		var manifest pluginManifest
		if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
			return pluginManifest{}, fmt.Errorf("invalid manifest json")
		}
		if err := manifest.validate(); err != nil {
			return pluginManifest{}, err
		}
		return manifest, nil
	}
	return pluginManifest{}, fmt.Errorf("manifest.json is required")
}

func (m pluginManifest) validate() error {
	if strings.TrimSpace(m.PluginCode) == "" {
		return fmt.Errorf("plugin_code is required")
	}
	if normalizePluginType(m.PluginType) == "" {
		return fmt.Errorf("plugin_type is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if !supportedImportedPluginType(normalizePluginType(m.PluginType)) {
		return fmt.Errorf("unsupported plugin_type")
	}
	return nil
}

func (m pluginManifest) toImportResponse(checksum string) PluginImportResponse {
	pluginType := normalizePluginType(m.PluginType)
	version := strings.TrimSpace(m.Version)
	runtime := strings.TrimSpace(m.Runtime)
	if runtime == "" {
		runtime = "go_builtin"
	}
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = strings.TrimSpace(m.PluginCode)
	}
	return PluginImportResponse{
		PluginCode:       strings.TrimSpace(m.PluginCode),
		PluginType:       pluginType,
		PluginVersion:    version,
		Name:             name,
		Description:      strings.TrimSpace(m.Description),
		Runtime:          runtime,
		Entrypoint:       strings.TrimSpace(m.Entrypoint),
		Status:           "disabled",
		Checksum:         checksum,
		ConfigSchema:     ensureSchemaMap(m.ConfigSchema),
		UISchema:         ensureSchemaMap(m.UISchema),
		InputSchema:      ensureSchemaMap(m.InputSchema),
		OutputSchema:     ensureSchemaMap(m.OutputSchema),
		PermissionSchema: ensureSchemaMap(m.PermissionSchema),
	}
}

func (p PluginImportResponse) key() string {
	return p.PluginType + "/" + p.PluginCode + "@" + p.PluginVersion
}

func (p PluginImportResponse) toStoreRecord() mysqlstore.PluginRecord {
	return mysqlstore.PluginRecord{
		PluginCode:       p.PluginCode,
		PluginType:       p.PluginType,
		PluginVersion:    p.PluginVersion,
		Name:             p.Name,
		Description:      p.Description,
		Runtime:          p.Runtime,
		Entrypoint:       p.Entrypoint,
		Status:           p.Status,
		Checksum:         p.Checksum,
		ConfigSchema:     mustJSONRaw(p.ConfigSchema),
		UISchema:         mustJSONRaw(p.UISchema),
		InputSchema:      mustJSONRaw(p.InputSchema),
		OutputSchema:     mustJSONRaw(p.OutputSchema),
		PermissionSchema: mustJSONRaw(p.PermissionSchema),
	}
}

func pluginChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizePluginType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "input", "collector", "collect":
		return "input"
	case "parser", "parse":
		return "parser"
	case "search_command", "search-command", "search":
		return "search_command"
	case "spl_function", "spl-function", "function":
		return "spl_function"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func supportedImportedPluginType(value string) bool {
	switch value {
	case "input", "parser", "search_command":
		return true
	default:
		return false
	}
}

func pluginResponsesFromMetadata(items []plugin.Metadata, filterType string) []PluginImportResponse {
	out := make([]PluginImportResponse, 0, len(items))
	for _, meta := range items {
		pluginType := normalizePluginType(string(meta.Type))
		if filterType != "" && pluginType != filterType {
			continue
		}
		if !productVisibleBuiltinPlugin(pluginType, meta.Code) {
			continue
		}
		status := "active"
		if strings.EqualFold(meta.Labels["status"], "disabled") {
			status = "disabled"
		}
		out = append(out, PluginImportResponse{
			PluginCode:       meta.Code,
			PluginType:       pluginType,
			PluginVersion:    meta.Version,
			Name:             firstNonEmpty(meta.Name, meta.Code),
			Description:      meta.Description,
			Runtime:          meta.Runtime,
			Status:           status,
			Checksum:         firstNonEmpty(meta.Labels["checksum"], "builtin"),
			ConfigSchema:     ensureSchemaMap(meta.ConfigSchema),
			UISchema:         ensureSchemaMap(meta.UISchema),
			InputSchema:      ensureSchemaMap(meta.InputSchema),
			OutputSchema:     ensureSchemaMap(meta.OutputSchema),
			PermissionSchema: ensureSchemaMap(meta.PermissionSchema),
		})
	}
	return out
}

func pluginResponsesFromRecords(items []mysqlstore.PluginRecord, filterType string) []PluginImportResponse {
	out := make([]PluginImportResponse, 0, len(items))
	for _, item := range items {
		pluginType := normalizePluginType(item.PluginType)
		if filterType != "" && pluginType != filterType {
			continue
		}
		if !productVisiblePluginRecord(pluginType, item) {
			continue
		}
		out = append(out, PluginImportResponse{
			PluginCode:       item.PluginCode,
			PluginType:       pluginType,
			PluginVersion:    item.PluginVersion,
			Name:             firstNonEmpty(item.Name, item.PluginCode),
			Description:      item.Description,
			Runtime:          item.Runtime,
			Entrypoint:       item.Entrypoint,
			Status:           firstNonEmpty(item.Status, "disabled"),
			Checksum:         item.Checksum,
			ConfigSchema:     rawToSchemaMap(item.ConfigSchema),
			UISchema:         rawToSchemaMap(item.UISchema),
			InputSchema:      rawToSchemaMap(item.InputSchema),
			OutputSchema:     rawToSchemaMap(item.OutputSchema),
			PermissionSchema: rawToSchemaMap(item.PermissionSchema),
		})
	}
	return out
}

func productVisiblePluginRecord(pluginType string, item mysqlstore.PluginRecord) bool {
	if pluginType == "spl_function" {
		return false
	}
	if hiddenProductPluginCode(pluginType, item.PluginCode) {
		return false
	}
	checksum := strings.TrimSpace(item.Checksum)
	if checksum != "" && checksum != "builtin" {
		return supportedImportedPluginType(pluginType)
	}
	return productVisibleBuiltinPlugin(pluginType, item.PluginCode)
}

func productVisibleBuiltinPlugin(pluginType, code string) bool {
	switch pluginType {
	case "input":
		return code == "syslog"
	case "parser":
		return code == "regex"
	case "search_command":
		return code == "stats"
	default:
		return false
	}
}

func hiddenProductPluginCode(pluginType, code string) bool {
	switch pluginType + "/" + strings.TrimSpace(code) {
	case "input/http-input", "parser/json-parser", "parser/props-conf-parser":
		return true
	default:
		return false
	}
}

func ensureSchemaMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func rawToSchemaMap(raw json.RawMessage) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func mustJSONRaw(value map[string]any) json.RawMessage {
	data, err := json.Marshal(ensureSchemaMap(value))
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}
