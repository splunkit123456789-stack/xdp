package event

import (
	"testing"
	"time"
)

func TestNewDefaultsParseStatusToUnparsed(t *testing.T) {
	ev := New("raw", Source{Type: "syslog", Name: "Firewall Syslog"}, time.Now().UTC())

	if ev.Metadata["parse_status"] != "unparsed" {
		t.Fatalf("parse_status = %#v, want unparsed", ev.Metadata["parse_status"])
	}
	if ev.Metadata["parse_rule_id"] != "" || ev.Metadata["parse_rule_name"] != "" || ev.Metadata["parse_error"] != "" {
		t.Fatalf("parse metadata defaults = %#v", ev.Metadata)
	}
}
