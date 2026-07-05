ALTER TABLE xdp.events_app
    ADD COLUMN IF NOT EXISTS parse_status LowCardinality(String) DEFAULT 'unparsed' AFTER sourcetype,
    ADD COLUMN IF NOT EXISTS parse_rule_id String DEFAULT '' AFTER parse_status,
    ADD COLUMN IF NOT EXISTS parse_rule_name String DEFAULT '' AFTER parse_rule_id,
    ADD COLUMN IF NOT EXISTS parse_error String DEFAULT '' AFTER parse_rule_name,
    ADD COLUMN IF NOT EXISTS parsed_at DateTime64(3, 'Asia/Shanghai') DEFAULT toDateTime64('1970-01-01 00:00:00', 3, 'Asia/Shanghai') AFTER parse_error;

ALTER TABLE xdp.events_firewall
    ADD COLUMN IF NOT EXISTS parse_status LowCardinality(String) DEFAULT 'unparsed' AFTER sourcetype,
    ADD COLUMN IF NOT EXISTS parse_rule_id String DEFAULT '' AFTER parse_status,
    ADD COLUMN IF NOT EXISTS parse_rule_name String DEFAULT '' AFTER parse_rule_id,
    ADD COLUMN IF NOT EXISTS parse_error String DEFAULT '' AFTER parse_rule_name,
    ADD COLUMN IF NOT EXISTS parsed_at DateTime64(3, 'Asia/Shanghai') DEFAULT toDateTime64('1970-01-01 00:00:00', 3, 'Asia/Shanghai') AFTER parse_error;
