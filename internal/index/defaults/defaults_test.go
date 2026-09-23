package defaults

import "testing"

func TestSame(t *testing.T) {
	yes := [][2]string{
		{"8080", "8080"}, {"`8080`", "8080"}, {"\":8080\"", ":8080"}, {"30s", "30s"}, {"30s", "30000ms"},
		{"1m", "60s"}, {"true", "true"}, {"yes", "true"}, {"off", "false"}, {"none", ""}, {"\"\"", "none"},
		{"1.0", "1"}, {"info", "info"},
	}
	no := [][2]string{{"8080", "9090"}, {"30s", "31s"}, {"info", "debug"}, {"true", "false"}, {"/docs", "/docs/"}}
	for _, c := range yes {
		if !Same(c[0], c[1]) {
			t.Errorf("Same(%q, %q) = false", c[0], c[1])
		}
	}
	for _, c := range no {
		if Same(c[0], c[1]) {
			t.Errorf("Same(%q, %q) = true", c[0], c[1])
		}
	}
}

func TestSet(t *testing.T) {
	s := New()
	s.Add("flag", "--Port_Num", "8080")
	s.Add("flag", "port-num", "1") // first wins
	s.Add("key", "Server.Addr", ":8080")
	s.Add("env", "HOME_DIR", "/tmp")
	s.Add("env", "", "x")
	if v, ok := s.Get("flag", "port_num"); !ok || v != "8080" {
		t.Errorf("flag = %q %v", v, ok)
	}
	if v, ok := s.Get("key", "server.addr"); !ok || v != ":8080" {
		t.Errorf("key = %q %v", v, ok)
	}
	if s.Len() != 3 {
		t.Errorf("len = %d, list = %v", s.Len(), s.List())
	}
}
