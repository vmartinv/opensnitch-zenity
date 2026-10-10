package rules

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

var now = time.Unix(1700000000, 0)

func curl() *pb.Connection {
	return &pb.Connection{
		Protocol: "tcp", DstIp: "93.184.216.34", DstHost: "example.com", DstPort: 443,
		UserId: 1000, ProcessId: 42, ProcessPath: "/usr/bin/curl",
	}
}

type op struct{ typ, operand, data string }

func flatten(o *pb.Operator) []op {
	if o.Type != "list" {
		return []op{{o.Type, o.Operand, o.Data}}
	}
	var out []op
	for _, l := range o.List {
		out = append(out, op{l.Type, l.Operand, l.Data})
	}
	return out
}

func TestBuildScopes(t *testing.T) {
	tests := []struct {
		scope Scope
		want  []op
	}{
		{ScopeProgram, []op{{"simple", "process.path", "/usr/bin/curl"}}},
		{ScopeHost, []op{{"simple", "dest.host", "example.com"}}},
		{ScopeProgramHost, []op{{"simple", "process.path", "/usr/bin/curl"}, {"simple", "dest.host", "example.com"}}},
	}
	for _, tt := range tests {
		t.Run(string(tt.scope), func(t *testing.T) {
			r, err := Build("allow", "always", tt.scope, curl(), now)
			if err != nil {
				t.Fatal(err)
			}
			got := flatten(r.Operator)
			if len(got) != len(tt.want) {
				t.Fatalf("operators = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("operator %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
			if !r.Enabled || r.Action != "allow" || r.Duration != "always" || r.Created != now.Unix() {
				t.Errorf("rule header wrong: %v", r)
			}
		})
	}
}

func TestListOperatorMatchesStockUI(t *testing.T) {
	r, err := Build("deny", "once", ScopeProgramHost, curl(), now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Operator.Type != "list" || r.Operator.Operand != "list" {
		t.Fatalf("type/operand = %s/%s, want list/list", r.Operator.Type, r.Operator.Operand)
	}
	var data []map[string]string
	if err := json.Unmarshal([]byte(r.Operator.Data), &data); err != nil {
		t.Fatalf("Data is not JSON: %v", err)
	}
	if len(data) != 2 || data[1]["operand"] != "dest.host" || data[1]["data"] != "example.com" {
		t.Errorf("Data = %s", r.Operator.Data)
	}
}

func TestBuildUsesIPWithoutHostname(t *testing.T) {
	c := curl()
	c.DstHost = ""
	r, err := Build("allow", "once", ScopeHost, c, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Operator.Operand != "dest.ip" || r.Operator.Data != "93.184.216.34" {
		t.Errorf("operator = %v", r.Operator)
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	noPath := curl()
	noPath.ProcessPath = ""
	tests := []struct {
		name             string
		action, duration string
		scope            Scope
		conn             *pb.Connection
	}{
		{"action", "accept", "once", ScopeProgram, curl()},
		{"duration", "allow", "forever", ScopeProgram, curl()},
		{"scope", "allow", "once", "everything", curl()},
		{"missing path", "allow", "once", ScopeProgram, noPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Build(tt.action, tt.duration, tt.scope, tt.conn, now); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestBuildAcceptsDaemonDurations(t *testing.T) {
	for _, d := range Durations {
		if _, err := Build("allow", d, ScopeProgram, curl(), now); err != nil {
			t.Errorf("duration %q: %v", d, err)
		}
	}
}

func TestNameIsFilesystemSafe(t *testing.T) {
	c := curl()
	c.ProcessPath = "/tmp/../evil prog/$(rm -rf)"
	c.DstHost = "a.b/../../etc"
	r, err := Build("allow", "always", ScopeProgramHost, c, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(r.Name, "/. $()") || r.Name == "" {
		t.Errorf("unsafe name %q", r.Name)
	}
	if !strings.HasPrefix(r.Name, "allow-always-list-") {
		t.Errorf("name %q does not follow stock UI pattern", r.Name)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"allow always simple /usr/bin/curl": "allow-always-simple-usr-bin-curl",
		"--Weird__ input!!":                 "weird-input",
		"":                                  "",
		strings.Repeat("a", 200):            strings.Repeat("a", 128),
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFallback(t *testing.T) {
	r := Fallback("deny", curl(), now)
	if r.Duration != "once" || r.Action != "deny" || !r.Enabled || r.Operator.Operand != "process.path" {
		t.Errorf("fallback = %v", r)
	}

	kernel := &pb.Connection{Protocol: "udp", DstIp: "1.1.1.1", DstPort: 53}
	if r := Fallback("allow", kernel, now); r.Operator.Operand != "dest.ip" {
		t.Errorf("fallback without process = %v", r.Operator)
	}

	empty := &pb.Connection{Protocol: "udp"}
	if r := Fallback("deny", empty, now); r.Operator == nil || r.Operator.Data != "udp" || r.Name == "" {
		t.Errorf("fallback for empty connection = %v", r)
	}
}
