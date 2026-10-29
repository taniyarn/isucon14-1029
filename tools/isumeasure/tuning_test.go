package main

import "testing"

func TestKnobWhen(t *testing.T) {
	for name, want := range map[string]string{
		"max_connections": "now", "table_open_cache": "now", "worker_connections": "now", "net.core.somaxconn / backlog": "now",
		"ip_local_port_range / tcp_tw_reuse": "now", "LimitNOFILE": "now", "SetMaxOpenConns / SetMaxIdleConns": "now",
		"innodb_buffer_pool_size": "later", "GOGC": "later", "innodb_flush_log_at_trx_commit": "later",
		"innodb_log_file_size / innodb_log_buffer_size": "later", "upstream keepalive": "later",
	} {
		if got := knobWhen(name); got != want {
			t.Errorf("knobWhen(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestFirstNum(t *testing.T) {
	for s, want := range map[string]float64{"4000": 4000, "1024 (used 900)": 1024, " 256": 256} {
		if got, ok := firstNum(s); !ok || got != want {
			t.Errorf("firstNum(%q) = %v %v, want %v", s, got, ok, want)
		}
	}
	if _, ok := firstNum("(no upstream keepalive)"); ok {
		t.Error("firstNum on text should fail")
	}
}
