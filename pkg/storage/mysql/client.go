package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"xdp/pkg/event"
	"xdp/pkg/pipeline"
	"xdp/pkg/plugin"
)

type Client struct{ db *sql.DB }

type Config struct{ DSN string }

type DataSource struct {
	Code          string
	Type          string
	Name          string
	Status        string
	Config        json.RawMessage
	ConfigVersion int64
	UpdatedAt     time.Time
}

type DataSourceRuntimeState struct {
	DataSourceID     string
	DataSourceCode   string
	AgentID          string
	PluginCode       string
	DesiredStatus    string
	RuntimeStatus    string
	ListenerStatus   string
	Protocol         string
	ListenHost       string
	ListenPort       int
	Endpoint         string
	PipelineID       string
	ConfigVersion    int64
	LastLoadedAt     *time.Time
	LastTransitionAt *time.Time
	LastHeartbeatAt  *time.Time
	LastErrorCode    string
	LastError        string
}

type IndexConfig struct {
	Code             string
	Name             string
	Status           string
	HotRetentionDays int
	UpdatedAt        time.Time
}

type IndexStorageSnapshot struct {
	IndexName       string
	TableName       string
	Rows            uint64
	StorageBytes    uint64
	LatestEventTime string
	CapturedAt      time.Time
}

type ParserPlugin struct {
	PluginCode          string
	PluginType          string
	DisplayName         string
	Category            string
	Description         string
	Version             string
	ConfigSchema        json.RawMessage
	ValidationRules     json.RawMessage
	PropsTemplate       string
	RuntimeCapabilities json.RawMessage
	Status              string
	Builtin             bool
}

type ParseRule struct {
	ID                  string
	Code                string
	Name                string
	Status              string
	ParserPlugin        string
	ParserPluginVersion string
	DataSourceID        string
	DataSourceName      string
	InputRoute          string
	OutputIndex         string
	Source              string
	Sourcetype          string
	Priority            int
	Stage               string
	SampleEvent         string
	PluginConfig        json.RawMessage
	PropsConf           string
	PreviewResult       json.RawMessage
	ValidationResult    json.RawMessage
	HotFields           json.RawMessage
	PipelineID          string
	LastPublishedAt     *time.Time
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SavedSearch struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	SPL           string    `json:"spl"`
	TimeRangeType string    `json:"time_range_type"`
	Earliest      string    `json:"earliest,omitempty"`
	Latest        string    `json:"latest,omitempty"`
	Visibility    string    `json:"visibility"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type AuthSeed struct {
	Username     string
	DisplayName  string
	PasswordHash string
	PasswordAlgo string
	RoleLabel    string
	TokenHash    string
	TokenPrefix  string
	Source       string
}

type PluginRecord struct {
	PluginCode       string
	PluginType       string
	PluginVersion    string
	Name             string
	Description      string
	Runtime          string
	Entrypoint       string
	Status           string
	Checksum         string
	Signature        string
	ConfigSchema     json.RawMessage
	UISchema         json.RawMessage
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	PermissionSchema json.RawMessage
	RuntimeConfig    json.RawMessage
	PackageBytes     []byte
}

type SearchCommandExecutionAudit struct {
	RequestID      string
	SearchID       string
	PluginType     string
	PluginCode     string
	PluginVersion  string
	CommandName    string
	Runtime        string
	Interpreter    string
	TimeoutMS      int
	MaxInputRows   int
	MaxOutputBytes int
	InputRows      int64
	OutputRows     int64
	ElapsedMS      int
	Success        bool
	ErrorCode      string
	ErrorMessage   string
	StdoutBytes    int64
	StderrBytes    int64
	CreatedAt      time.Time
}

func Open(cfg Config) (*Client, error) {
	dsn := cfg.DSN
	if dsn == "" {
		dsn = "xdp:xdp@tcp(127.0.0.1:3306)/xdp?parseTime=true&multiStatements=true"
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	return &Client{db: db}, nil
}

func New(db *sql.DB) *Client { return &Client{db: db} }

func (c *Client) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return c.db.PingContext(ctx)
}

func (c *Client) Migrate(ctx context.Context) error {
	if c == nil || c.db == nil {
		return nil
	}
	if _, err := c.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	_, err := c.db.ExecContext(ctx, compatibilitySchema)
	return err
}

func (c *Client) UpsertPlugin(ctx context.Context, meta plugin.Metadata) error {
	configSchema, _ := json.Marshal(meta.ConfigSchema)
	uiSchema, _ := json.Marshal(meta.UISchema)
	inputSchema, _ := json.Marshal(meta.InputSchema)
	outputSchema, _ := json.Marshal(meta.OutputSchema)
	permissionSchema, _ := json.Marshal(meta.PermissionSchema)
	_, err := c.db.ExecContext(ctx, `INSERT INTO plugins (id, type, code, name, description, status) VALUES (UUID(), ?, ?, ?, ?, 'active') ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), status = 'active', deleted_at = NULL`, string(meta.Type), meta.Code, meta.Name, meta.Description)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO plugin_versions (id, plugin_id, version, runtime, config_schema, ui_schema, input_schema, output_schema, permission_schema, status) SELECT UUID(), id, ?, ?, ?, ?, ?, ?, ?, 'active' FROM plugins WHERE type = ? AND code = ? ON DUPLICATE KEY UPDATE version = VALUES(version), runtime = VALUES(runtime), config_schema = VALUES(config_schema), ui_schema = VALUES(ui_schema), input_schema = VALUES(input_schema), output_schema = VALUES(output_schema), permission_schema = VALUES(permission_schema), status = 'active'`, meta.Version, meta.Runtime, string(configSchema), string(uiSchema), string(inputSchema), string(outputSchema), string(permissionSchema), string(meta.Type), meta.Code)
	return err
}

func (c *Client) ListPlugins(ctx context.Context) ([]plugin.Metadata, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT p.type, p.code, p.name, COALESCE(p.description, ''), pv.version, pv.runtime, pv.config_schema, pv.ui_schema, pv.input_schema, pv.output_schema, pv.permission_schema FROM plugins p JOIN plugin_versions pv ON pv.plugin_id = p.id WHERE p.status = 'active' AND pv.status = 'active' ORDER BY p.type, p.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []plugin.Metadata{}
	for rows.Next() {
		var meta plugin.Metadata
		var typ, configSchema, uiSchema, inputSchema, outputSchema, permissionSchema string
		if err := rows.Scan(&typ, &meta.Code, &meta.Name, &meta.Description, &meta.Version, &meta.Runtime, &configSchema, &uiSchema, &inputSchema, &outputSchema, &permissionSchema); err != nil {
			return nil, err
		}
		meta.Type = plugin.Type(typ)
		_ = json.Unmarshal([]byte(configSchema), &meta.ConfigSchema)
		_ = json.Unmarshal([]byte(uiSchema), &meta.UISchema)
		_ = json.Unmarshal([]byte(inputSchema), &meta.InputSchema)
		_ = json.Unmarshal([]byte(outputSchema), &meta.OutputSchema)
		_ = json.Unmarshal([]byte(permissionSchema), &meta.PermissionSchema)
		items = append(items, meta)
	}
	return items, rows.Err()
}

func (c *Client) UpsertPluginRecord(ctx context.Context, item PluginRecord) error {
	if item.Status == "" {
		item.Status = "disabled"
	}
	if len(item.ConfigSchema) == 0 {
		item.ConfigSchema = json.RawMessage(`{}`)
	}
	if len(item.UISchema) == 0 {
		item.UISchema = json.RawMessage(`{}`)
	}
	if len(item.InputSchema) == 0 {
		item.InputSchema = json.RawMessage(`{}`)
	}
	if len(item.OutputSchema) == 0 {
		item.OutputSchema = json.RawMessage(`{}`)
	}
	if len(item.PermissionSchema) == 0 {
		item.PermissionSchema = json.RawMessage(`{}`)
	}
	if len(item.RuntimeConfig) == 0 {
		item.RuntimeConfig = json.RawMessage(`{}`)
	}
	if item.Runtime == "" {
		item.Runtime = "go_builtin"
	}
	if item.PluginVersion == "" {
		item.PluginVersion = "1.0.0"
	}
	if item.Name == "" {
		item.Name = item.PluginCode
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO plugins (id, type, code, name, description, status) VALUES (UUID(), ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), status = VALUES(status), deleted_at = NULL`, item.PluginType, item.PluginCode, item.Name, item.Description, item.Status)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `INSERT INTO plugin_versions (id, plugin_id, version, runtime, entrypoint, config_schema, ui_schema, input_schema, output_schema, permission_schema, runtime_config, package_bytes, checksum, signature, status) SELECT UUID(), id, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ? FROM plugins WHERE type = ? AND code = ? ON DUPLICATE KEY UPDATE version = VALUES(version), runtime = VALUES(runtime), entrypoint = VALUES(entrypoint), config_schema = VALUES(config_schema), ui_schema = VALUES(ui_schema), input_schema = VALUES(input_schema), output_schema = VALUES(output_schema), permission_schema = VALUES(permission_schema), runtime_config = VALUES(runtime_config), package_bytes = VALUES(package_bytes), checksum = VALUES(checksum), signature = VALUES(signature), status = VALUES(status)`, item.PluginVersion, item.Runtime, item.Entrypoint, string(item.ConfigSchema), string(item.UISchema), string(item.InputSchema), string(item.OutputSchema), string(item.PermissionSchema), string(item.RuntimeConfig), item.PackageBytes, item.Checksum, item.Signature, item.Status, item.PluginType, item.PluginCode)
	return err
}

func (c *Client) ListPluginRecords(ctx context.Context) ([]PluginRecord, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT p.type, p.code, p.name, COALESCE(p.description, ''), p.status, pv.version, pv.runtime, COALESCE(pv.entrypoint, ''), pv.config_schema, pv.ui_schema, pv.input_schema, pv.output_schema, pv.permission_schema, COALESCE(pv.runtime_config, JSON_OBJECT()), COALESCE(pv.package_bytes, ''), COALESCE(pv.checksum, ''), COALESCE(pv.signature, ''), pv.status FROM plugins p JOIN plugin_versions pv ON pv.plugin_id = p.id WHERE p.deleted_at IS NULL ORDER BY p.type, p.code, pv.version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PluginRecord{}
	for rows.Next() {
		var item PluginRecord
		if err := rows.Scan(&item.PluginType, &item.PluginCode, &item.Name, &item.Description, &item.Status, &item.PluginVersion, &item.Runtime, &item.Entrypoint, &item.ConfigSchema, &item.UISchema, &item.InputSchema, &item.OutputSchema, &item.PermissionSchema, &item.RuntimeConfig, &item.PackageBytes, &item.Checksum, &item.Signature, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) GetPluginRecord(ctx context.Context, pluginType string, pluginCode string, version string) (PluginRecord, error) {
	query := `SELECT p.type, p.code, p.name, COALESCE(p.description, ''), p.status, pv.version, pv.runtime, COALESCE(pv.entrypoint, ''), pv.config_schema, pv.ui_schema, pv.input_schema, pv.output_schema, pv.permission_schema, COALESCE(pv.runtime_config, JSON_OBJECT()), COALESCE(pv.package_bytes, ''), COALESCE(pv.checksum, ''), COALESCE(pv.signature, ''), pv.status
FROM plugins p
JOIN plugin_versions pv ON pv.plugin_id = p.id
WHERE p.deleted_at IS NULL AND p.type = ? AND p.code = ?`
	args := []any{pluginType, pluginCode}
	query += ` ORDER BY CASE WHEN pv.status IN ('enabled', 'active') THEN 0 ELSE 1 END, pv.version DESC LIMIT 1`
	row := c.db.QueryRowContext(ctx, query, args...)
	var item PluginRecord
	if err := row.Scan(&item.PluginType, &item.PluginCode, &item.Name, &item.Description, &item.Status, &item.PluginVersion, &item.Runtime, &item.Entrypoint, &item.ConfigSchema, &item.UISchema, &item.InputSchema, &item.OutputSchema, &item.PermissionSchema, &item.RuntimeConfig, &item.PackageBytes, &item.Checksum, &item.Signature, &item.Status); err != nil {
		return PluginRecord{}, err
	}
	return item, nil
}

func (c *Client) SetPluginStatus(ctx context.Context, pluginType string, pluginCode string, status string) (PluginRecord, error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return PluginRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE plugin_versions pv JOIN plugins p ON pv.plugin_id = p.id SET pv.status = ? WHERE p.type = ? AND p.code = ?`, status, pluginType, pluginCode)
	if err != nil {
		return PluginRecord{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return PluginRecord{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plugins SET status = ? WHERE type = ? AND code = ?`, status, pluginType, pluginCode); err != nil {
		return PluginRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return PluginRecord{}, err
	}
	return c.GetPluginRecord(ctx, pluginType, pluginCode, "")
}

func (c *Client) SaveSearchCommandExecutionAudit(ctx context.Context, item SearchCommandExecutionAudit) error {
	if c == nil || c.db == nil {
		return nil
	}
	if item.PluginType == "" {
		item.PluginType = "search_command"
	}
	if item.RequestID == "" {
		item.RequestID = fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO search_command_execution_audits (id, request_id, search_id, plugin_type, plugin_code, plugin_version, command_name, runtime, interpreter, timeout_ms, max_input_rows, max_output_bytes, input_rows, output_rows, elapsed_ms, success, error_code, error_message, stdout_bytes, stderr_bytes) VALUES (UUID(), ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		item.RequestID,
		item.SearchID,
		item.PluginType,
		item.PluginCode,
		item.PluginVersion,
		item.CommandName,
		item.Runtime,
		item.Interpreter,
		item.TimeoutMS,
		item.MaxInputRows,
		item.MaxOutputBytes,
		item.InputRows,
		item.OutputRows,
		item.ElapsedMS,
		item.Success,
		item.ErrorCode,
		item.ErrorMessage,
		item.StdoutBytes,
		item.StderrBytes,
	)
	return err
}

func (c *Client) ListSearchCommandExecutionAudits(ctx context.Context, pluginCode string, limit int) ([]SearchCommandExecutionAudit, error) {
	if c == nil || c.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := c.db.QueryContext(ctx, `SELECT request_id, COALESCE(search_id, ''), plugin_type, plugin_code, plugin_version, command_name, runtime, COALESCE(interpreter, ''), timeout_ms, max_input_rows, max_output_bytes, input_rows, output_rows, elapsed_ms, success, COALESCE(error_code, ''), COALESCE(error_message, ''), stdout_bytes, stderr_bytes, created_at FROM search_command_execution_audits WHERE plugin_type = 'search_command' AND plugin_code = ? ORDER BY created_at DESC LIMIT ?`, pluginCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SearchCommandExecutionAudit{}
	for rows.Next() {
		var item SearchCommandExecutionAudit
		if err := rows.Scan(&item.RequestID, &item.SearchID, &item.PluginType, &item.PluginCode, &item.PluginVersion, &item.CommandName, &item.Runtime, &item.Interpreter, &item.TimeoutMS, &item.MaxInputRows, &item.MaxOutputBytes, &item.InputRows, &item.OutputRows, &item.ElapsedMS, &item.Success, &item.ErrorCode, &item.ErrorMessage, &item.StdoutBytes, &item.StderrBytes, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) DeletePlugin(ctx context.Context, pluginType string, pluginCode string) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `DELETE pv FROM plugin_versions pv JOIN plugins p ON pv.plugin_id = p.id WHERE p.type = ? AND p.code = ? AND pv.status NOT IN ('enabled', 'active')`, pluginType, pluginCode)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plugins WHERE type = ? AND code = ? AND status NOT IN ('enabled', 'active')`, pluginType, pluginCode); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) SavePipeline(ctx context.Context, pipe pipeline.Pipeline) error {
	spec, err := json.Marshal(pipe)
	if err != nil {
		return err
	}
	compiled := spec
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `INSERT INTO pipelines (id, code, name, description, status) VALUES (UUID(), ?, ?, ?, 'published') ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), status = 'published'`, pipe.Metadata.ID, pipe.Metadata.Name, pipe.Metadata.Description); err != nil {
		return err
	}

	var pipelineID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM pipelines WHERE code = ?`, pipe.Metadata.ID).Scan(&pipelineID); err != nil {
		return err
	}

	var latestID sql.NullString
	var latestSpec sql.NullString
	var latestVersion sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT pv.id, pv.spec, pv.version FROM pipeline_versions pv WHERE pv.pipeline_id = ? ORDER BY pv.version DESC LIMIT 1`, pipelineID).Scan(&latestID, &latestSpec, &latestVersion); err != nil && err != sql.ErrNoRows {
		return err
	}
	if latestSpec.Valid && latestSpec.String == string(spec) {
		if latestID.Valid {
			if _, err = tx.ExecContext(ctx, `UPDATE pipelines SET current_version_id = ?, status = 'published' WHERE id = ? AND (current_version_id IS NULL OR current_version_id <> ?)`, latestID.String, pipelineID, latestID.String); err != nil {
				return err
			}
		}
		return tx.Commit()
	}

	version := 1
	if latestVersion.Valid {
		version = int(latestVersion.Int64) + 1
	}
	var versionID string
	if err = tx.QueryRowContext(ctx, `SELECT UUID()`).Scan(&versionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pipeline_versions (id, pipeline_id, version, version_name, spec, compiled_spec, status) VALUES (?, ?, ?, ?, ?, ?, 'published')`, versionID, pipelineID, version, fmt.Sprintf("v%d", version), string(spec), string(compiled)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pipelines SET current_version_id = ?, status = 'published' WHERE id = ?`, versionID, pipelineID); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) ListPipelines(ctx context.Context) ([]pipeline.Pipeline, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT pv.spec FROM pipelines p JOIN pipeline_versions pv ON pv.id = p.current_version_id WHERE p.status = 'published' AND pv.status = 'published' ORDER BY p.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []pipeline.Pipeline{}
	for rows.Next() {
		var specText string
		if err := rows.Scan(&specText); err != nil {
			return nil, err
		}
		var pipe pipeline.Pipeline
		if err := json.Unmarshal([]byte(specText), &pipe); err == nil {
			items = append(items, pipe)
		}
	}
	return items, rows.Err()
}

func (c *Client) SeedDataSource(ctx context.Context, item DataSource) error {
	if item.Status == "" {
		item.Status = "active"
	}
	if item.ConfigVersion <= 0 {
		item.ConfigVersion = 1
	}
	_, err := c.db.ExecContext(ctx, `INSERT IGNORE INTO data_sources (id, code, type, name, config_json, status, config_version) VALUES (UUID(), ?, ?, ?, ?, ?, ?)`, item.Code, item.Type, item.Name, string(item.Config), item.Status, item.ConfigVersion)
	return err
}

const saveDataSourceUpdateSQL = `UPDATE data_sources
SET type = ?,
    name = ?,
    config_json = ?,
    status = ?,
    config_version = config_version + 1,
    deleted_at = CASE WHEN ? = 'deleted' THEN CURRENT_TIMESTAMP(3) ELSE NULL END,
    updated_at = CURRENT_TIMESTAMP(3)
WHERE code = ?`

const saveDataSourceInsertSQL = `INSERT INTO data_sources (id, code, type, name, config_json, status, config_version, deleted_at)
VALUES (UUID(), ?, ?, ?, ?, ?, ?, CASE WHEN ? = 'deleted' THEN CURRENT_TIMESTAMP(3) ELSE NULL END)`

func (c *Client) SaveDataSource(ctx context.Context, item DataSource) error {
	if item.Status == "" {
		item.Status = "active"
	}
	if item.ConfigVersion <= 0 {
		item.ConfigVersion = 1
	}
	result, err := c.db.ExecContext(ctx, saveDataSourceUpdateSQL, item.Type, item.Name, string(item.Config), item.Status, item.Status, item.Code)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	_, err = c.db.ExecContext(ctx, saveDataSourceInsertSQL, item.Code, item.Type, item.Name, string(item.Config), item.Status, item.ConfigVersion, item.Status)
	return err
}

func (c *Client) ListDataSources(ctx context.Context) ([]DataSource, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT code, type, name, status, config_json, config_version, updated_at FROM data_sources WHERE deleted_at IS NULL ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []DataSource{}
	for rows.Next() {
		var item DataSource
		var configText string
		if err := rows.Scan(&item.Code, &item.Type, &item.Name, &item.Status, &configText, &item.ConfigVersion, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Config = json.RawMessage(configText)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) SaveDataSourceRuntimeState(ctx context.Context, item DataSourceRuntimeState) error {
	if item.DataSourceID == "" {
		return fmt.Errorf("data source id is required")
	}
	if item.AgentID == "" {
		item.AgentID = "local-agent"
	}
	if item.ConfigVersion <= 0 {
		item.ConfigVersion = 1
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO data_source_runtime_states (id, data_source_id, data_source_code, agent_id, plugin_code, desired_status, runtime_status, listener_status, protocol, listen_host, listen_port, endpoint, pipeline_id, config_version, last_loaded_at, last_transition_at, last_heartbeat_at, last_error_code, last_error) VALUES (UUID(), ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, '')) ON DUPLICATE KEY UPDATE data_source_code = VALUES(data_source_code), plugin_code = VALUES(plugin_code), desired_status = VALUES(desired_status), runtime_status = VALUES(runtime_status), listener_status = VALUES(listener_status), protocol = VALUES(protocol), listen_host = VALUES(listen_host), listen_port = VALUES(listen_port), endpoint = VALUES(endpoint), pipeline_id = VALUES(pipeline_id), config_version = VALUES(config_version), last_loaded_at = VALUES(last_loaded_at), last_transition_at = VALUES(last_transition_at), last_heartbeat_at = VALUES(last_heartbeat_at), last_error_code = VALUES(last_error_code), last_error = VALUES(last_error), updated_at = CURRENT_TIMESTAMP(3)`, item.DataSourceID, item.DataSourceCode, item.AgentID, item.PluginCode, item.DesiredStatus, item.RuntimeStatus, item.ListenerStatus, item.Protocol, item.ListenHost, item.ListenPort, item.Endpoint, item.PipelineID, item.ConfigVersion, item.LastLoadedAt, item.LastTransitionAt, item.LastHeartbeatAt, item.LastErrorCode, item.LastError)
	return err
}

const seedSavedSearchesCountSQL = `SELECT COUNT(*) FROM saved_searches`

const seedSavedSearchesInsertSQL = `INSERT IGNORE INTO saved_searches (id, name, spl, time_range_type, visibility, status)
VALUES (?, ?, ?, ?, ?, ?)`

func (c *Client) SeedSavedSearches(ctx context.Context, items []SavedSearch) error {
	if len(items) == 0 {
		return nil
	}
	var count int
	if err := c.db.QueryRowContext(ctx, seedSavedSearchesCountSQL).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, item := range items {
		if item.ID == "" || item.SPL == "" {
			continue
		}
		if item.Name == "" {
			item.Name = item.SPL
		}
		if item.TimeRangeType == "" {
			item.TimeRangeType = "近 1 天"
		}
		if item.Visibility == "" {
			item.Visibility = "private"
		}
		if item.Status == "" {
			item.Status = "active"
		}
		if _, err := c.db.ExecContext(ctx, seedSavedSearchesInsertSQL, item.ID, item.Name, item.SPL, item.TimeRangeType, item.Visibility, item.Status); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) UpsertIndexConfig(ctx context.Context, item IndexConfig) error {
	if item.Status == "" {
		item.Status = "active"
	}
	if item.Name == "" {
		item.Name = item.Code
	}
	if item.HotRetentionDays <= 0 {
		item.HotRetentionDays = 30
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO indexes (id, code, name, hot_retention_days, status) VALUES (UUID(), ?, ?, ?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name), hot_retention_days = VALUES(hot_retention_days), status = VALUES(status), deleted_at = NULL, updated_at = CURRENT_TIMESTAMP(3)`, item.Code, item.Name, item.HotRetentionDays, item.Status)
	return err
}

func (c *Client) DeleteIndexConfig(ctx context.Context, code string) error {
	_, err := c.db.ExecContext(ctx, `UPDATE indexes SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE code = ?`, code)
	return err
}

func (c *Client) ListIndexConfigs(ctx context.Context) ([]IndexConfig, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT code, name, status, hot_retention_days, updated_at FROM indexes WHERE status = 'active' AND deleted_at IS NULL ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IndexConfig{}
	for rows.Next() {
		var item IndexConfig
		if err := rows.Scan(&item.Code, &item.Name, &item.Status, &item.HotRetentionDays, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) InsertIndexStorageSnapshots(ctx context.Context, items []IndexStorageSnapshot) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO index_storage_snapshots (id, index_name, table_name, row_count, storage_bytes, latest_event_time, captured_at) VALUES (UUID(), ?, ?, ?, ?, NULLIF(?, ''), ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, item := range items {
		capturedAt := item.CapturedAt
		if capturedAt.IsZero() {
			capturedAt = time.Now()
		}
		if _, err := stmt.ExecContext(ctx, item.IndexName, item.TableName, item.Rows, item.StorageBytes, item.LatestEventTime, capturedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *Client) ListIndexStorageSnapshots(ctx context.Context, indexName string, since time.Time) ([]IndexStorageSnapshot, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT index_name, table_name, row_count, storage_bytes, COALESCE(DATE_FORMAT(latest_event_time, '%Y-%m-%d %H:%i:%s'), ''), captured_at FROM index_storage_snapshots WHERE index_name = ? AND captured_at >= ? ORDER BY captured_at ASC`, indexName, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IndexStorageSnapshot{}
	for rows.Next() {
		var item IndexStorageSnapshot
		if err := rows.Scan(&item.IndexName, &item.TableName, &item.Rows, &item.StorageBytes, &item.LatestEventTime, &item.CapturedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) PruneIndexStorageSnapshots(ctx context.Context, before time.Time) (int64, error) {
	result, err := c.db.ExecContext(ctx, `DELETE FROM index_storage_snapshots WHERE captured_at < ?`, before)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return affected, nil
}

func (c *Client) SeedParserPlugins(ctx context.Context, items []ParserPlugin) error {
	for _, item := range items {
		if item.PluginType == "" {
			item.PluginType = "parser"
		}
		if item.Version == "" {
			item.Version = "1.0.0"
		}
		if item.Status == "" {
			item.Status = "active"
		}
		if len(item.ConfigSchema) == 0 {
			item.ConfigSchema = json.RawMessage(`{}`)
		}
		if len(item.ValidationRules) == 0 {
			item.ValidationRules = json.RawMessage(`{}`)
		}
		if len(item.RuntimeCapabilities) == 0 {
			item.RuntimeCapabilities = json.RawMessage(`{}`)
		}
		_, err := c.db.ExecContext(ctx, `INSERT INTO parser_plugins (id, plugin_code, plugin_type, display_name, category, description, version, config_schema, validation_rules, props_template, runtime_capabilities, status, builtin) VALUES (UUID(), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE plugin_type = VALUES(plugin_type), display_name = VALUES(display_name), category = VALUES(category), description = VALUES(description), config_schema = VALUES(config_schema), validation_rules = VALUES(validation_rules), props_template = VALUES(props_template), runtime_capabilities = VALUES(runtime_capabilities), status = VALUES(status), builtin = VALUES(builtin), updated_at = CURRENT_TIMESTAMP(3)`, item.PluginCode, item.PluginType, item.DisplayName, item.Category, item.Description, item.Version, string(item.ConfigSchema), string(item.ValidationRules), item.PropsTemplate, string(item.RuntimeCapabilities), item.Status, item.Builtin)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) SaveParseRule(ctx context.Context, item ParseRule) error {
	if item.ID == "" {
		return fmt.Errorf("parse rule id is required")
	}
	if item.Status == "" {
		item.Status = "active"
	}
	if item.ParserPluginVersion == "" {
		item.ParserPluginVersion = "1.0.0"
	}
	if item.InputRoute == "" {
		item.InputRoute = "internal_raw_topic"
	}
	if item.OutputIndex == "" {
		item.OutputIndex = "app"
	}
	if item.Stage == "" {
		item.Stage = "ingest"
	}
	if item.Priority == 0 {
		item.Priority = 100
	}
	if len(item.PluginConfig) == 0 {
		item.PluginConfig = json.RawMessage(`{}`)
	}
	if len(item.PreviewResult) == 0 {
		item.PreviewResult = json.RawMessage(`[]`)
	}
	if len(item.ValidationResult) == 0 {
		item.ValidationResult = json.RawMessage(`{}`)
	}
	if len(item.HotFields) == 0 {
		item.HotFields = json.RawMessage(`[]`)
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO parse_rules (id, code, name, status, parser_plugin, parser_plugin_version, data_source_id, data_source_name, input_route, output_index, source, sourcetype, priority, stage, sample_event, plugin_config, props_conf, preview_result, validation_result, hot_fields, pipeline_id, last_published_at, last_error) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, '')) ON DUPLICATE KEY UPDATE code = VALUES(code), name = VALUES(name), status = VALUES(status), parser_plugin = VALUES(parser_plugin), parser_plugin_version = VALUES(parser_plugin_version), data_source_id = VALUES(data_source_id), data_source_name = VALUES(data_source_name), input_route = VALUES(input_route), output_index = VALUES(output_index), source = VALUES(source), sourcetype = VALUES(sourcetype), priority = VALUES(priority), stage = VALUES(stage), sample_event = VALUES(sample_event), plugin_config = VALUES(plugin_config), props_conf = VALUES(props_conf), preview_result = VALUES(preview_result), validation_result = VALUES(validation_result), hot_fields = VALUES(hot_fields), pipeline_id = VALUES(pipeline_id), last_published_at = VALUES(last_published_at), last_error = VALUES(last_error), deleted_at = NULL, updated_at = CURRENT_TIMESTAMP(3)`, item.ID, item.Code, item.Name, item.Status, item.ParserPlugin, item.ParserPluginVersion, item.DataSourceID, item.DataSourceName, item.InputRoute, item.OutputIndex, item.Source, item.Sourcetype, item.Priority, item.Stage, item.SampleEvent, string(item.PluginConfig), item.PropsConf, string(item.PreviewResult), string(item.ValidationResult), string(item.HotFields), item.PipelineID, item.LastPublishedAt, item.LastError)
	return err
}

func (c *Client) ListParseRules(ctx context.Context) ([]ParseRule, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, code, name, status, parser_plugin, parser_plugin_version, COALESCE(data_source_id, ''), COALESCE(data_source_name, ''), input_route, output_index, COALESCE(source, ''), COALESCE(sourcetype, ''), priority, stage, COALESCE(sample_event, ''), plugin_config, props_conf, preview_result, validation_result, COALESCE(hot_fields, JSON_ARRAY()), COALESCE(pipeline_id, ''), last_published_at, COALESCE(last_error, ''), created_at, updated_at FROM parse_rules WHERE status <> 'deleted' AND deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ParseRule{}
	for rows.Next() {
		item, err := scanParseRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) GetParseRule(ctx context.Context, id string) (ParseRule, error) {
	row := c.db.QueryRowContext(ctx, `SELECT id, code, name, status, parser_plugin, parser_plugin_version, COALESCE(data_source_id, ''), COALESCE(data_source_name, ''), input_route, output_index, COALESCE(source, ''), COALESCE(sourcetype, ''), priority, stage, COALESCE(sample_event, ''), plugin_config, props_conf, preview_result, validation_result, COALESCE(hot_fields, JSON_ARRAY()), COALESCE(pipeline_id, ''), last_published_at, COALESCE(last_error, ''), created_at, updated_at FROM parse_rules WHERE id = ? AND status <> 'deleted' AND deleted_at IS NULL`, id)
	return scanParseRule(row)
}

func (c *Client) DeleteParseRule(ctx context.Context, id string) error {
	_, err := c.db.ExecContext(ctx, `UPDATE parse_rules SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ?`, id)
	return err
}

func (c *Client) SaveSavedSearch(ctx context.Context, item SavedSearch) (SavedSearch, error) {
	if item.ID == "" {
		if err := c.db.QueryRowContext(ctx, `SELECT UUID()`).Scan(&item.ID); err != nil {
			return SavedSearch{}, err
		}
	}
	if item.TimeRangeType == "" {
		item.TimeRangeType = "近 1 天"
	}
	if item.Visibility == "" {
		item.Visibility = "private"
	}
	if item.Status == "" {
		item.Status = "active"
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO saved_searches (id, name, description, spl, time_range_type, earliest, latest, visibility, status) VALUES (?, ?, NULLIF(?, ''), ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?) ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description), spl = VALUES(spl), time_range_type = VALUES(time_range_type), earliest = VALUES(earliest), latest = VALUES(latest), visibility = VALUES(visibility), status = VALUES(status), deleted_at = NULL, updated_at = CURRENT_TIMESTAMP(3)`, item.ID, item.Name, item.Description, item.SPL, item.TimeRangeType, item.Earliest, item.Latest, item.Visibility, item.Status)
	if err != nil {
		return SavedSearch{}, err
	}
	saved, err := c.GetSavedSearch(ctx, item.ID)
	if err != nil {
		return SavedSearch{}, err
	}
	return saved, nil
}

func (c *Client) ListSavedSearches(ctx context.Context) ([]SavedSearch, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, name, COALESCE(description, ''), spl, time_range_type, COALESCE(earliest, ''), COALESCE(latest, ''), visibility, status, created_at, updated_at FROM saved_searches WHERE status = 'active' AND deleted_at IS NULL ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SavedSearch{}
	for rows.Next() {
		item, err := scanSavedSearch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (c *Client) GetSavedSearch(ctx context.Context, id string) (SavedSearch, error) {
	row := c.db.QueryRowContext(ctx, `SELECT id, name, COALESCE(description, ''), spl, time_range_type, COALESCE(earliest, ''), COALESCE(latest, ''), visibility, status, created_at, updated_at FROM saved_searches WHERE id = ? AND status = 'active' AND deleted_at IS NULL`, id)
	return scanSavedSearch(row)
}

func (c *Client) DeleteSavedSearch(ctx context.Context, id string) error {
	result, err := c.db.ExecContext(ctx, `UPDATE saved_searches SET status = 'deleted', deleted_at = CURRENT_TIMESTAMP(3), updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type parseRuleScanner interface {
	Scan(dest ...any) error
}

func scanParseRule(scanner parseRuleScanner) (ParseRule, error) {
	var item ParseRule
	var pluginConfig, previewResult, validationResult, hotFields string
	var lastPublishedAt sql.NullTime
	if err := scanner.Scan(&item.ID, &item.Code, &item.Name, &item.Status, &item.ParserPlugin, &item.ParserPluginVersion, &item.DataSourceID, &item.DataSourceName, &item.InputRoute, &item.OutputIndex, &item.Source, &item.Sourcetype, &item.Priority, &item.Stage, &item.SampleEvent, &pluginConfig, &item.PropsConf, &previewResult, &validationResult, &hotFields, &item.PipelineID, &lastPublishedAt, &item.LastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return ParseRule{}, err
	}
	item.PluginConfig = json.RawMessage(pluginConfig)
	item.PreviewResult = json.RawMessage(previewResult)
	item.ValidationResult = json.RawMessage(validationResult)
	item.HotFields = json.RawMessage(hotFields)
	if lastPublishedAt.Valid {
		item.LastPublishedAt = &lastPublishedAt.Time
	}
	return item, nil
}

type savedSearchScanner interface {
	Scan(dest ...any) error
}

func scanSavedSearch(scanner savedSearchScanner) (SavedSearch, error) {
	var item SavedSearch
	if err := scanner.Scan(&item.ID, &item.Name, &item.Description, &item.SPL, &item.TimeRangeType, &item.Earliest, &item.Latest, &item.Visibility, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return SavedSearch{}, err
	}
	return item, nil
}

func (c *Client) SaveDeadletter(ctx context.Context, e *event.Event) error {
	if e == nil {
		return nil
	}
	snapshot, _ := json.Marshal(e)
	errorCode := "UNKNOWN"
	errorMessage := ""
	stage := ""
	pluginCode := ""
	if len(e.Errors) > 0 {
		last := e.Errors[len(e.Errors)-1]
		errorCode = last.ErrorCode
		errorMessage = last.Message
		stage = last.Stage
		pluginCode = last.PluginID
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO deadletter_records (id, event_id, stage_id, plugin_code, error_code, error_message, raw_preview, event_snapshot, status) VALUES (UUID(), ?, ?, ?, ?, ?, ?, ?, 'pending')`, e.EventID, stage, pluginCode, errorCode, errorMessage, preview(e.Raw), string(snapshot))
	return err
}

func (c *Client) ListDeadletters(ctx context.Context) ([]*event.Event, error) {
	query := `SELECT event_snapshot FROM deadletter_records ORDER BY last_seen_at DESC LIMIT 100`
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*event.Event{}
	for rows.Next() {
		var snapshot string
		if err := rows.Scan(&snapshot); err != nil {
			return nil, err
		}
		var e event.Event
		if err := json.Unmarshal([]byte(snapshot), &e); err == nil {
			items = append(items, &e)
		}
	}
	return items, rows.Err()
}

func preview(raw string) string {
	if len(raw) > 512 {
		return raw[:512]
	}
	return raw
}

func (c *Client) SeedPlugins(ctx context.Context, metas []plugin.Metadata) error {
	for _, meta := range metas {
		if err := c.UpsertPlugin(ctx, meta); err != nil {
			return fmt.Errorf("seed plugin %s: %w", meta.Code, err)
		}
	}
	return nil
}

func (c *Client) SeedAuth(ctx context.Context, seed AuthSeed) error {
	if seed.Username == "" || seed.PasswordHash == "" || seed.TokenHash == "" {
		return fmt.Errorf("auth seed requires username, password hash and token hash")
	}
	if seed.DisplayName == "" {
		seed.DisplayName = seed.Username
	}
	if seed.PasswordAlgo == "" {
		seed.PasswordAlgo = "bcrypt"
	}
	if seed.RoleLabel == "" {
		seed.RoleLabel = "admin"
	}
	if seed.Source == "" {
		seed.Source = "env_seed"
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_users (id, username, display_name, password_hash, password_algo, role_label, status) VALUES (UUID(), ?, ?, ?, ?, ?, 'active') ON DUPLICATE KEY UPDATE display_name = VALUES(display_name), password_hash = VALUES(password_hash), password_algo = VALUES(password_algo), role_label = VALUES(role_label), status = 'active', updated_at = CURRENT_TIMESTAMP(3), deleted_at = NULL`, seed.Username, seed.DisplayName, seed.PasswordHash, seed.PasswordAlgo, seed.RoleLabel); err != nil {
		return err
	}
	var userID string
	if err = tx.QueryRowContext(ctx, `SELECT id FROM auth_users WHERE username = ?`, seed.Username).Scan(&userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_tokens (id, user_id, token_name, token_hash, token_prefix, token_type, source, status) VALUES (UUID(), ?, 'default', ?, ?, 'bearer', ?, 'active') ON DUPLICATE KEY UPDATE user_id = VALUES(user_id), token_prefix = VALUES(token_prefix), source = VALUES(source), status = 'active', revoked_at = NULL, updated_at = CURRENT_TIMESTAMP(3)`, userID, seed.TokenHash, seed.TokenPrefix, seed.Source); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Client) ValidateAuthCredentials(ctx context.Context, username string, password string) (bool, error) {
	var passwordHash, passwordAlgo string
	if err := c.db.QueryRowContext(ctx, `SELECT password_hash, password_algo FROM auth_users WHERE username = ? AND status = 'active' AND deleted_at IS NULL`, username).Scan(&passwordHash, &passwordAlgo); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	ok := false
	switch passwordAlgo {
	case "bcrypt":
		ok = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) == nil
	case "sha256":
		sum := sha256.Sum256([]byte(password))
		ok = passwordHash == hex.EncodeToString(sum[:])
	}
	if ok {
		_, _ = c.db.ExecContext(ctx, `UPDATE auth_users SET last_login_at = CURRENT_TIMESTAMP(3), failed_login_count = 0 WHERE username = ?`, username)
		return true, nil
	}
	_, _ = c.db.ExecContext(ctx, `UPDATE auth_users SET failed_login_count = failed_login_count + 1 WHERE username = ?`, username)
	return false, nil
}

func (c *Client) ValidateAuthToken(ctx context.Context, tokenHash string) (bool, error) {
	var count int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_tokens tok JOIN auth_users usr ON usr.id = tok.user_id WHERE tok.token_hash = ? AND tok.status = 'active' AND usr.status = 'active' AND usr.deleted_at IS NULL AND (tok.expires_at IS NULL OR tok.expires_at > CURRENT_TIMESTAMP(3))`, tokenHash).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		_, _ = c.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at = CURRENT_TIMESTAMP(3) WHERE token_hash = ?`, tokenHash)
	}
	return count > 0, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS auth_users (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    username VARCHAR(128) NOT NULL,
    display_name VARCHAR(255) NULL,
    password_hash VARCHAR(255) NOT NULL,
    password_algo VARCHAR(64) NOT NULL DEFAULT 'bcrypt',
    role_label VARCHAR(64) NOT NULL DEFAULT 'admin',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    last_login_at DATETIME(3) NULL,
    failed_login_count INT NOT NULL DEFAULT 0,
    locked_until DATETIME(3) NULL,
    metadata JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    UNIQUE KEY uk_auth_users_username (username)
);
CREATE TABLE IF NOT EXISTS auth_tokens (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    user_id CHAR(36) NOT NULL,
    token_name VARCHAR(128) NOT NULL DEFAULT 'default',
    token_hash VARCHAR(255) NOT NULL,
    token_prefix VARCHAR(32) NULL,
    token_type VARCHAR(32) NOT NULL DEFAULT 'bearer',
    source VARCHAR(32) NOT NULL DEFAULT 'login',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    expires_at DATETIME(3) NULL,
    last_used_at DATETIME(3) NULL,
    revoked_at DATETIME(3) NULL,
    metadata JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_auth_tokens_hash (token_hash),
    KEY idx_auth_tokens_user_status (user_id, status),
    CONSTRAINT fk_auth_tokens_user FOREIGN KEY (user_id) REFERENCES auth_users(id)
);
CREATE TABLE IF NOT EXISTS auth_audit_logs (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    user_id CHAR(36) NULL,
    username VARCHAR(128) NULL,
    event_type VARCHAR(64) NOT NULL,
    result VARCHAR(32) NOT NULL,
    request_id VARCHAR(128) NULL,
    source_ip VARCHAR(64) NULL,
    user_agent VARCHAR(512) NULL,
    method VARCHAR(16) NULL,
    path VARCHAR(512) NULL,
    error_code VARCHAR(64) NULL,
    metadata JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    KEY idx_auth_audit_created_at (created_at),
    KEY idx_auth_audit_username_type (username, event_type)
);
CREATE TABLE IF NOT EXISTS plugins (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    type VARCHAR(64) NOT NULL,
    code VARCHAR(128) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    UNIQUE KEY uk_plugins_type_code (type, code)
);
CREATE TABLE IF NOT EXISTS plugin_versions (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    plugin_id CHAR(36) NOT NULL,
    version VARCHAR(64) NOT NULL,
    runtime VARCHAR(32) NOT NULL DEFAULT 'go',
    entrypoint VARCHAR(512) NULL,
    config_schema JSON NOT NULL DEFAULT (JSON_OBJECT()),
    ui_schema JSON NOT NULL DEFAULT (JSON_OBJECT()),
    input_schema JSON NOT NULL DEFAULT (JSON_OBJECT()),
    output_schema JSON NOT NULL DEFAULT (JSON_OBJECT()),
	    permission_schema JSON NOT NULL DEFAULT (JSON_OBJECT()),
	    runtime_config JSON NOT NULL DEFAULT (JSON_OBJECT()),
	    package_bytes LONGBLOB NULL,
	    checksum VARCHAR(128) NULL,
    signature VARCHAR(255) NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_plugin_versions_plugin (plugin_id),
    CONSTRAINT fk_plugin_versions_plugin FOREIGN KEY (plugin_id) REFERENCES plugins(id)
);
CREATE TABLE IF NOT EXISTS search_command_execution_audits (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    request_id VARCHAR(128) NOT NULL,
    search_id VARCHAR(128) NULL,
    plugin_type VARCHAR(32) NOT NULL DEFAULT 'search_command',
    plugin_code VARCHAR(128) NOT NULL,
    plugin_version VARCHAR(64) NOT NULL,
    command_name VARCHAR(128) NOT NULL,
    runtime VARCHAR(64) NOT NULL,
    interpreter VARCHAR(64) NULL,
    timeout_ms INT NOT NULL DEFAULT 5000,
    max_input_rows INT NOT NULL DEFAULT 10000,
    max_output_bytes INT NOT NULL DEFAULT 4194304,
    input_rows BIGINT NOT NULL DEFAULT 0,
    output_rows BIGINT NOT NULL DEFAULT 0,
    elapsed_ms INT NOT NULL DEFAULT 0,
    success TINYINT(1) NOT NULL DEFAULT 0,
    error_code VARCHAR(64) NULL,
    error_message TEXT NULL,
    stdout_bytes BIGINT NOT NULL DEFAULT 0,
    stderr_bytes BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    KEY idx_search_command_audit_plugin_time (plugin_code, created_at),
    KEY idx_search_command_audit_request (request_id),
    KEY idx_search_command_audit_success_time (success, created_at)
);
CREATE TABLE IF NOT EXISTS pipelines (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    code VARCHAR(128) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    current_version_id CHAR(36) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    UNIQUE KEY uk_pipelines_code (code)
);
CREATE TABLE IF NOT EXISTS pipeline_versions (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    pipeline_id CHAR(36) NOT NULL,
    version INT NOT NULL,
    version_name VARCHAR(64) NOT NULL,
    spec JSON NOT NULL,
    compiled_spec JSON NOT NULL DEFAULT (JSON_OBJECT()),
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    validation_result JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_pipeline_versions_pipeline_version (pipeline_id, version),
    CONSTRAINT fk_pipeline_versions_pipeline FOREIGN KEY (pipeline_id) REFERENCES pipelines(id)
);
CREATE TABLE IF NOT EXISTS indexes (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    code VARCHAR(128) NOT NULL,
    name VARCHAR(255) NOT NULL,
    storage_hot VARCHAR(64) NOT NULL DEFAULT 'clickhouse',
    storage_cold VARCHAR(64) NULL DEFAULT 's3',
    hot_retention_days INT NOT NULL DEFAULT 30,
    cold_retention_days INT NULL DEFAULT 180,
    schema_json JSON NOT NULL DEFAULT (JSON_OBJECT()),
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    UNIQUE KEY uk_indexes_code (code)
);
CREATE TABLE IF NOT EXISTS index_storage_snapshots (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    index_name VARCHAR(128) NOT NULL,
    table_name VARCHAR(255) NOT NULL,
    row_count BIGINT NOT NULL DEFAULT 0,
    storage_bytes BIGINT NOT NULL DEFAULT 0,
    latest_event_time DATETIME(3) NULL,
    captured_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    KEY idx_index_storage_snapshots_name_time (index_name, captured_at),
    KEY idx_index_storage_snapshots_table_time (table_name, captured_at)
);
CREATE TABLE IF NOT EXISTS parser_plugins (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    plugin_code VARCHAR(128) NOT NULL,
    plugin_type VARCHAR(32) NOT NULL DEFAULT 'parser',
    display_name VARCHAR(255) NOT NULL,
    category VARCHAR(64) NOT NULL,
    description TEXT NULL,
    version VARCHAR(64) NOT NULL DEFAULT '1.0.0',
    config_schema JSON NOT NULL,
    validation_rules JSON NOT NULL DEFAULT (JSON_OBJECT()),
    props_template TEXT NULL,
    runtime_capabilities JSON NOT NULL DEFAULT (JSON_OBJECT()),
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    builtin TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_parser_plugins_code_version (plugin_code, version)
);
CREATE TABLE IF NOT EXISTS parse_rules (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    code VARCHAR(128) NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    parser_plugin VARCHAR(128) NOT NULL,
    parser_plugin_version VARCHAR(64) NOT NULL DEFAULT '1.0.0',
    data_source_id CHAR(36) NULL,
    data_source_name VARCHAR(255) NULL,
    input_route VARCHAR(255) NOT NULL DEFAULT 'internal_raw_topic',
    output_index VARCHAR(128) NOT NULL DEFAULT 'app',
    source VARCHAR(255) NULL,
    sourcetype VARCHAR(128) NULL,
    priority INT NOT NULL DEFAULT 100,
    stage VARCHAR(32) NOT NULL DEFAULT 'ingest',
    sample_event TEXT NULL,
    plugin_config JSON NOT NULL,
    props_conf MEDIUMTEXT NOT NULL,
    preview_result JSON NOT NULL DEFAULT (JSON_ARRAY()),
    validation_result JSON NOT NULL DEFAULT (JSON_OBJECT()),
    hot_fields JSON NOT NULL DEFAULT (JSON_ARRAY()),
    pipeline_id CHAR(36) NULL,
    last_published_at DATETIME(3) NULL,
    last_error TEXT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    UNIQUE KEY uk_parse_rules_code (code),
    KEY idx_parse_rules_plugin_status (parser_plugin, status),
    KEY idx_parse_rules_route_status_priority (input_route, status, priority, code),
    KEY idx_parse_rules_output_index_status (output_index, status),
    KEY idx_parse_rules_sourcetype (sourcetype)
);
CREATE TABLE IF NOT EXISTS data_sources (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    code VARCHAR(128) NOT NULL,
    type VARCHAR(64) NOT NULL,
    name VARCHAR(255) NOT NULL,
    config_json JSON NOT NULL DEFAULT (JSON_OBJECT()),
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    config_version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    active_name VARCHAR(255) GENERATED ALWAYS AS (CASE WHEN deleted_at IS NULL THEN name ELSE NULL END) STORED,
    UNIQUE KEY uk_data_sources_code (code),
    UNIQUE KEY uk_data_sources_active_name (active_name),
    KEY idx_data_sources_status_updated (status, updated_at)
);
CREATE TABLE IF NOT EXISTS data_source_runtime_states (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    data_source_id CHAR(36) NOT NULL,
    data_source_code VARCHAR(128) NOT NULL,
    agent_id VARCHAR(128) NOT NULL DEFAULT 'local-agent',
    plugin_code VARCHAR(128) NOT NULL,
    desired_status VARCHAR(32) NOT NULL,
    runtime_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    listener_status VARCHAR(32) NOT NULL DEFAULT 'unknown',
    protocol VARCHAR(16) NULL,
    listen_host VARCHAR(128) NULL,
    listen_port INT NULL,
    endpoint VARCHAR(255) NULL,
    pipeline_id CHAR(36) NULL,
    config_version BIGINT NOT NULL DEFAULT 1,
    last_loaded_at DATETIME(3) NULL,
    last_transition_at DATETIME(3) NULL,
    last_heartbeat_at DATETIME(3) NULL,
    last_error_code VARCHAR(128) NULL,
    last_error TEXT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_runtime_state_source_agent (data_source_id, agent_id),
    KEY idx_runtime_state_status (runtime_status, listener_status),
    KEY idx_runtime_state_heartbeat (last_heartbeat_at)
);
CREATE TABLE IF NOT EXISTS saved_searches (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    name VARCHAR(128) NOT NULL,
    description TEXT NULL,
    spl TEXT NOT NULL,
    time_range_type VARCHAR(32) NOT NULL DEFAULT 'preset',
    earliest VARCHAR(64) NULL,
    latest VARCHAR(64) NULL,
    visibility VARCHAR(32) NOT NULL DEFAULT 'private',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_by CHAR(36) NULL,
    last_run_at DATETIME(3) NULL,
    run_count BIGINT NOT NULL DEFAULT 0,
    metadata JSON NOT NULL DEFAULT (JSON_OBJECT()),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    deleted_at DATETIME(3) NULL,
    KEY idx_saved_searches_owner_time (created_by, created_at DESC),
    KEY idx_saved_searches_status_time (status, updated_at DESC)
);
CREATE TABLE IF NOT EXISTS deadletter_records (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    event_id VARCHAR(64) NULL,
    stage_id VARCHAR(128) NULL,
    plugin_code VARCHAR(128) NULL,
    error_code VARCHAR(64) NOT NULL,
    error_message TEXT NOT NULL,
    retryable TINYINT(1) NOT NULL DEFAULT 0,
    raw_preview TEXT NULL,
    event_snapshot JSON NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    first_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    resolved_at DATETIME(3) NULL
);
`

const compatibilitySchema = `
DROP PROCEDURE IF EXISTS xdp_ensure_data_sources_compat;
CREATE PROCEDURE xdp_ensure_data_sources_compat()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'data_sources')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'data_sources' AND column_name = 'config_version') THEN
        ALTER TABLE data_sources ADD COLUMN config_version BIGINT NOT NULL DEFAULT 1 AFTER status;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'data_sources')
       AND NOT EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'data_sources' AND index_name = 'idx_data_sources_status_updated') THEN
        ALTER TABLE data_sources ADD KEY idx_data_sources_status_updated (status, updated_at);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'data_sources')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'data_sources' AND column_name = 'active_name') THEN
        ALTER TABLE data_sources ADD COLUMN active_name VARCHAR(255) GENERATED ALWAYS AS (CASE WHEN deleted_at IS NULL THEN name ELSE NULL END) STORED AFTER deleted_at;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'data_sources')
       AND NOT EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'data_sources' AND index_name = 'uk_data_sources_active_name') THEN
        ALTER TABLE data_sources ADD UNIQUE KEY uk_data_sources_active_name (active_name);
    END IF;
END;
CALL xdp_ensure_data_sources_compat();
DROP PROCEDURE IF EXISTS xdp_ensure_data_sources_compat;

DROP PROCEDURE IF EXISTS xdp_ensure_plugin_versions_compat;
CREATE PROCEDURE xdp_ensure_plugin_versions_compat()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND column_name = 'ui_schema') THEN
        ALTER TABLE plugin_versions ADD COLUMN ui_schema JSON NOT NULL DEFAULT (JSON_OBJECT()) AFTER config_schema;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND column_name = 'checksum') THEN
        ALTER TABLE plugin_versions ADD COLUMN checksum VARCHAR(128) NULL AFTER permission_schema;
    END IF;
	IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
	   AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND column_name = 'signature') THEN
	    ALTER TABLE plugin_versions ADD COLUMN signature VARCHAR(255) NULL AFTER checksum;
	END IF;
	IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
	   AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND column_name = 'runtime_config') THEN
	    ALTER TABLE plugin_versions ADD COLUMN runtime_config JSON NOT NULL DEFAULT (JSON_OBJECT()) AFTER permission_schema;
	END IF;
	IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
	   AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND column_name = 'package_bytes') THEN
	    ALTER TABLE plugin_versions ADD COLUMN package_bytes LONGBLOB NULL AFTER runtime_config;
	END IF;
	IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'plugin_versions')
	   AND NOT EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND index_name = 'uk_plugin_versions_plugin') THEN
	    DELETE older
        FROM plugin_versions older
        JOIN plugin_versions newer
          ON older.plugin_id = newer.plugin_id
         AND (
              older.updated_at < newer.updated_at
              OR (older.updated_at = newer.updated_at AND older.id < newer.id)
         );
        ALTER TABLE plugin_versions ADD UNIQUE KEY uk_plugin_versions_plugin (plugin_id);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'plugin_versions' AND index_name = 'uk_plugin_versions_plugin_version') THEN
        ALTER TABLE plugin_versions DROP INDEX uk_plugin_versions_plugin_version;
    END IF;
END;
CALL xdp_ensure_plugin_versions_compat();
DROP PROCEDURE IF EXISTS xdp_ensure_plugin_versions_compat;

DROP PROCEDURE IF EXISTS xdp_ensure_parse_rules_compat;
CREATE PROCEDURE xdp_ensure_parse_rules_compat()
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'parse_rules')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'parse_rules' AND column_name = 'output_index') THEN
        ALTER TABLE parse_rules ADD COLUMN output_index VARCHAR(128) NOT NULL DEFAULT 'app' AFTER input_route;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'parse_rules')
       AND NOT EXISTS (SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'parse_rules' AND index_name = 'idx_parse_rules_output_index_status') THEN
        ALTER TABLE parse_rules ADD KEY idx_parse_rules_output_index_status (output_index, status);
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'parse_rules')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'parse_rules' AND column_name = 'hot_fields') THEN
        ALTER TABLE parse_rules ADD COLUMN hot_fields JSON NOT NULL DEFAULT (JSON_ARRAY()) AFTER validation_result;
    END IF;
END;
CALL xdp_ensure_parse_rules_compat();
DROP PROCEDURE IF EXISTS xdp_ensure_parse_rules_compat;
`
