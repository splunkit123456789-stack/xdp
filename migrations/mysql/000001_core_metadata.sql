CREATE TABLE auth_users (
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

CREATE TABLE auth_tokens (
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

CREATE TABLE auth_audit_logs (
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

CREATE TABLE plugins (
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

CREATE TABLE plugin_versions (
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

CREATE TABLE pipelines (
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

CREATE TABLE pipeline_versions (
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

CREATE TABLE indexes (
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

CREATE TABLE index_storage_snapshots (
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

CREATE TABLE parser_plugins (
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
CREATE TABLE parse_rules (
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
CREATE TABLE data_sources (
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
    UNIQUE KEY uk_data_sources_active_name (active_name)
);

CREATE TABLE data_source_runtime_states (
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

CREATE TABLE saved_searches (
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
