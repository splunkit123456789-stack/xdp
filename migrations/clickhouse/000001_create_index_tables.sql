CREATE DATABASE IF NOT EXISTS xdp;

CREATE TABLE IF NOT EXISTS xdp.events_app
(
    index_name LowCardinality(String),
    event_id String,
    event_date Date DEFAULT toDate(event_time),
    event_time DateTime64(3, 'Asia/Shanghai'),
    ingest_time DateTime64(3, 'Asia/Shanghai'),
    pipeline_id String,
    pipeline_version String,
    source_type LowCardinality(String),
    source_name String,
    source_host String,
    source_ip String,
    sourcetype LowCardinality(String),
    parse_status LowCardinality(String) DEFAULT 'unparsed',
    parse_rule_id String DEFAULT '',
    parse_rule_name String DEFAULT '',
    parse_error String DEFAULT '',
    parsed_at DateTime64(3, 'Asia/Shanghai') DEFAULT toDateTime64('1970-01-01 00:00:00', 3, 'Asia/Shanghai'),
    vendor LowCardinality(String),
    product LowCardinality(String),
    raw String,
    raw_length UInt32 DEFAULT length(raw),
    fields_json String,
    labels_json String,
    tags Array(String),
    errors_json String,
    has_error UInt8 DEFAULT if(length(errors_json) > 2, 1, 0),
    created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai')
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(event_date)
ORDER BY (event_time, event_id)
TTL event_date + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS xdp.events_firewall AS xdp.events_app;
