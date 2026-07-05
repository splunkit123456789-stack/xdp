ALTER TABLE xdp.events_app
    MODIFY COLUMN event_time DateTime64(3, 'Asia/Shanghai'),
    MODIFY COLUMN ingest_time DateTime64(3, 'Asia/Shanghai'),
    MODIFY COLUMN created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai');

ALTER TABLE xdp.events_firewall
    MODIFY COLUMN event_time DateTime64(3, 'Asia/Shanghai'),
    MODIFY COLUMN ingest_time DateTime64(3, 'Asia/Shanghai'),
    MODIFY COLUMN created_at DateTime64(3, 'Asia/Shanghai') DEFAULT now64(3, 'Asia/Shanghai');
