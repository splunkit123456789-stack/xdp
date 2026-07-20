package mysql

import (
	"os"
	"strings"
	"testing"
)

func TestRuntimeSchemaIncludesAuthTables(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS auth_users",
		"password_hash VARCHAR(255) NOT NULL",
		"password_algo VARCHAR(64) NOT NULL DEFAULT 'bcrypt'",
		"UNIQUE KEY uk_auth_users_username",
		"CREATE TABLE IF NOT EXISTS auth_tokens",
		"token_hash VARCHAR(255) NOT NULL",
		"UNIQUE KEY uk_auth_tokens_hash",
		"CONSTRAINT fk_auth_tokens_user",
		"CREATE TABLE IF NOT EXISTS auth_audit_logs",
		"KEY idx_auth_audit_username_type",
	}
	for _, want := range required {
		if !strings.Contains(schema+" "+compatibilitySchema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesParseRuleOutputIndex(t *testing.T) {
	required := []string{
		"output_index VARCHAR(128) NOT NULL DEFAULT 'app'",
		"data_source_id VARCHAR(128) NULL",
		"pipeline_id VARCHAR(128) NULL",
		"ALTER TABLE parse_rules MODIFY COLUMN data_source_id VARCHAR(128) NULL",
		"KEY idx_parse_rules_output_index_status (output_index, status)",
		"ALTER TABLE parse_rules MODIFY COLUMN pipeline_id VARCHAR(128) NULL",
	}
	for _, want := range required {
		if !strings.Contains(schema+" "+compatibilitySchema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesDataSourceNameUniqueness(t *testing.T) {
	required := []string{
		"active_name VARCHAR(255) GENERATED ALWAYS AS (CASE WHEN deleted_at IS NULL THEN name ELSE NULL END) STORED",
		"UNIQUE KEY uk_data_sources_active_name (active_name)",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesDataSourceRuntimePipelineIDLength(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS data_source_runtime_states",
		"data_source_id VARCHAR(128) NOT NULL",
		"pipeline_id VARCHAR(128) NULL",
		"ALTER TABLE data_source_runtime_states MODIFY COLUMN data_source_id VARCHAR(128) NOT NULL",
		"ALTER TABLE data_source_runtime_states MODIFY COLUMN pipeline_id VARCHAR(128) NULL",
	}
	for _, want := range required {
		if !strings.Contains(schema+" "+compatibilitySchema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesSavedSearches(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS saved_searches",
		"spl TEXT NOT NULL",
		"time_range_type VARCHAR(32) NOT NULL DEFAULT 'preset'",
		"KEY idx_saved_searches_status_time (status, updated_at DESC)",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesIndexStorageSnapshots(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS index_storage_snapshots",
		"index_name VARCHAR(128) NOT NULL",
		"row_count BIGINT NOT NULL DEFAULT 0",
		"storage_bytes BIGINT NOT NULL DEFAULT 0",
		"KEY idx_index_storage_snapshots_name_time (index_name, captured_at)",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesPluginRuntimeConfig(t *testing.T) {
	required := []string{
		"runtime_config JSON NOT NULL DEFAULT (JSON_OBJECT())",
		"package_bytes LONGBLOB NULL",
		"ALTER TABLE plugin_versions ADD COLUMN runtime_config JSON NOT NULL DEFAULT (JSON_OBJECT()) AFTER permission_schema",
		"ALTER TABLE plugin_versions ADD COLUMN package_bytes LONGBLOB NULL AFTER runtime_config",
	}
	for _, want := range required {
		if !strings.Contains(schema+" "+compatibilitySchema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestRuntimeSchemaIncludesSearchCommandExecutionAudits(t *testing.T) {
	required := []string{
		"CREATE TABLE IF NOT EXISTS search_command_execution_audits",
		"plugin_code VARCHAR(128) NOT NULL",
		"timeout_ms INT NOT NULL DEFAULT 5000",
		"max_input_rows INT NOT NULL DEFAULT 10000",
		"max_output_bytes INT NOT NULL DEFAULT 4194304",
		"KEY idx_search_command_audit_plugin_time (plugin_code, created_at)",
		"KEY idx_search_command_audit_request (request_id)",
	}
	for _, want := range required {
		if !strings.Contains(schema, want) {
			t.Fatalf("runtime schema missing %q", want)
		}
	}
}

func TestSeedSavedSearchesOnlySeedsAnEmptyTable(t *testing.T) {
	if !strings.Contains(seedSavedSearchesCountSQL, "COUNT(*) FROM saved_searches") {
		t.Fatalf("seed saved searches must check whether table is empty first")
	}
	if !strings.Contains(seedSavedSearchesInsertSQL, "INSERT IGNORE INTO saved_searches") {
		t.Fatalf("seed saved searches must insert defaults without overwriting existing rows")
	}
	if strings.Contains(seedSavedSearchesInsertSQL, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("seed saved searches must not resurrect deleted defaults via upsert")
	}
}

func TestSaveDataSourceSQLMaintainsDeletedAtLifecycle(t *testing.T) {
	required := map[string]string{
		"update marks deleted rows": "deleted_at = CASE WHEN ? = 'deleted' THEN CURRENT_TIMESTAMP(3) ELSE NULL END",
		"insert marks deleted rows": "CASE WHEN ? = 'deleted' THEN CURRENT_TIMESTAMP(3) ELSE NULL END",
	}
	for name, want := range required {
		if !strings.Contains(saveDataSourceUpdateSQL+" "+saveDataSourceInsertSQL, want) {
			t.Fatalf("%s: save datasource SQL missing %q", name, want)
		}
	}
	if strings.Contains(saveDataSourceInsertSQL, "ON DUPLICATE KEY UPDATE") {
		t.Fatalf("save datasource insert must not upsert on arbitrary unique-key conflicts")
	}
}

func TestRuntimeSchemaContainsNoWorkspaceIsolationArtifacts(t *testing.T) {
	forbidden := "ten" + "ant"
	for _, ddl := range []struct {
		name string
		body string
	}{
		{name: "runtime schema", body: schema},
		{name: "runtime compatibility schema", body: compatibilitySchema},
	} {
		if strings.Contains(strings.ToLower(ddl.body), forbidden) {
			t.Fatalf("%s must not contain workspace isolation artifacts", ddl.name)
		}
	}
}

func TestMigrationFileIncludesAuthTables(t *testing.T) {
	data, err := os.ReadFile("../../../migrations/mysql/000001_core_metadata.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	migration := string(data)
	required := []string{
		"CREATE TABLE auth_users",
		"password_hash VARCHAR(255) NOT NULL",
		"password_algo VARCHAR(64) NOT NULL DEFAULT 'bcrypt'",
		"CREATE TABLE auth_tokens",
		"token_hash VARCHAR(255) NOT NULL",
		"CREATE TABLE auth_audit_logs",
		"output_index VARCHAR(128) NOT NULL DEFAULT 'app'",
		"pipeline_id VARCHAR(128) NULL",
		"KEY idx_parse_rules_output_index_status (output_index, status)",
		"active_name VARCHAR(255) GENERATED ALWAYS AS (CASE WHEN deleted_at IS NULL THEN name ELSE NULL END) STORED",
		"UNIQUE KEY uk_data_sources_active_name (active_name)",
		"data_source_id VARCHAR(128) NULL",
		"CREATE TABLE saved_searches",
		"KEY idx_saved_searches_status_time (status, updated_at DESC)",
		"CREATE TABLE index_storage_snapshots",
		"row_count BIGINT NOT NULL DEFAULT 0",
		"KEY idx_index_storage_snapshots_name_time (index_name, captured_at)",
		"CREATE TABLE data_source_runtime_states",
		"data_source_id VARCHAR(128) NOT NULL",
		"pipeline_id VARCHAR(128) NULL",
	}
	for _, want := range required {
		if !strings.Contains(migration, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(migration, "password VARCHAR") {
		t.Fatalf("migration must not define plaintext password column")
	}
}
