package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestTopicRouterReloadUsesInternalRawTopic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"datasources":[{"type":"syslog","status":"active","internal_raw_topic":"raw.ds_firewall"}]}`))
	}))
	defer server.Close()

	router := newTopicRouter()
	if err := router.reload(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if got := router.topic("syslog"); got != "raw.ds_firewall" {
		t.Fatalf("topic(syslog) = %q, want raw.ds_firewall", got)
	}
}

func TestTopicRouterReloadIgnoresLegacyRawTopic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"datasources":[{"type":"syslog","status":"active","raw_topic":"legacy.raw.topic"}]}`))
	}))
	defer server.Close()

	router := newTopicRouter()
	if err := router.reload(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if got := router.topic("syslog"); got != "xdp.raw.syslog" {
		t.Fatalf("topic(syslog) = %q, want default syslog topic", got)
	}
}

func TestTopicRouterReloadBuildsActiveSyslogListenerSpecs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"datasources":[{"id":"firewall-syslog","type":"syslog","name":"Firewall Syslog","status":"active","internal_raw_topic":"raw.ds_firewall","plugin_config":{"collector_port":5514,"transport_protocol":"UDP"}}]}`))
	}))
	defer server.Close()

	router := newTopicRouter()
	if err := router.reload(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	specs := router.syslogSpecs()
	if len(specs) != 1 {
		t.Fatalf("syslog specs len = %d, want 1: %#v", len(specs), specs)
	}
	spec := specs["firewall-syslog"]
	if spec.ID != "firewall-syslog" || spec.Addr != ":5514" || spec.Protocol != "udp" || spec.Name != "Firewall Syslog" || spec.Topic != "raw.ds_firewall" {
		t.Fatalf("syslog spec = %#v", spec)
	}
}

func TestTopicRouterReloadRemovesDisabledSyslogListenerSpecs(t *testing.T) {
	body := `{"datasources":[{"id":"firewall-syslog","type":"syslog","name":"Firewall Syslog","status":"active","internal_raw_topic":"raw.ds_firewall","plugin_config":{"collector_port":5514,"transport_protocol":"UDP"}}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	router := newTopicRouter()
	if err := router.reload(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("reload active failed: %v", err)
	}
	if len(router.syslogSpecs()) != 1 {
		t.Fatalf("expected active spec before disable")
	}

	body = `{"datasources":[{"id":"firewall-syslog","type":"syslog","name":"Firewall Syslog","status":"disabled","internal_raw_topic":"raw.ds_firewall","plugin_config":{"collector_port":5514,"transport_protocol":"UDP"}}]}`
	if err := router.reload(t.Context(), server.URL, ""); err != nil {
		t.Fatalf("reload disabled failed: %v", err)
	}
	if specs := router.syslogSpecs(); len(specs) != 0 {
		t.Fatalf("syslog specs after disabled = %#v, want empty", specs)
	}
}

func TestAgentManagementPortCheckRejectsOccupiedUDPPort(t *testing.T) {
	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer listener.Close()
	port := listener.LocalAddr().(*net.UDPAddr).Port

	server := httptest.NewServer(newAgentManagementHandler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/port-check", "application/json", bytes.NewBufferString(`{"plugin_code":"syslog","transport_protocol":"UDP","collector_port":`+strconv.Itoa(port)+`}`))
	if err != nil {
		t.Fatalf("post port check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Error.Code != "LISTENER_PORT_UNAVAILABLE" || body.Error.Message != "端口不可用" {
		t.Fatalf("error response = %#v", body)
	}
}

func TestAgentManagementPortCheckAcceptsAvailableUDPPort(t *testing.T) {
	listener, err := net.ListenPacket("udp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("find udp port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	server := httptest.NewServer(newAgentManagementHandler())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/port-check", "application/json", bytes.NewBufferString(`{"plugin_code":"syslog","transport_protocol":"UDP","collector_port":`+strconv.Itoa(port)+`}`))
	if err != nil {
		t.Fatalf("post port check: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var body struct {
		Available         bool   `json:"available"`
		CollectorPort     int    `json:"collector_port"`
		TransportProtocol string `json:"transport_protocol"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode port check response: %v", err)
	}
	if !body.Available || body.CollectorPort != port || body.TransportProtocol != "UDP" {
		t.Fatalf("port check response = %#v", body)
	}
}
