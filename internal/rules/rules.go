// Package rules builds protocol.Rule replies in the same shape the stock
// OpenSnitch UI (opensnitch/dialogs/prompt) sends back to the daemon.
package rules

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

const (
	ActionAllow  = "allow"
	ActionDeny   = "deny"
	ActionReject = "reject"

	DurationOnce         = "once"
	DurationUntilRestart = "until restart"
	DurationAlways       = "always"

	typeSimple = "simple"
	typeList   = "list"

	operandProcessPath = "process.path"
	operandDestHost    = "dest.host"
	operandDestIP      = "dest.ip"
)

// Actions in the order they are offered; the first one is preselected.
var Actions = []string{ActionDeny, ActionAllow, ActionReject}

// Durations accepted by opensnitchd 1.7.
var Durations = []string{DurationOnce, "30s", "5m", "15m", "30m", "1h", "12h", DurationUntilRestart, DurationAlways}

type Scope string

const (
	ScopeProgram     Scope = "program"
	ScopeProgramHost Scope = "program+host"
	ScopeHost        Scope = "host"
)

func ValidAction(a string) bool {
	for _, v := range Actions {
		if a == v {
			return true
		}
	}
	return false
}

func validDuration(d string) bool {
	for _, v := range Durations {
		if d == v {
			return true
		}
	}
	return false
}

// Build returns the rule for the user's choice on conn.
func Build(action, duration string, scope Scope, conn *pb.Connection, now time.Time) (*pb.Rule, error) {
	if !ValidAction(action) {
		return nil, fmt.Errorf("invalid action %q", action)
	}
	if !validDuration(duration) {
		return nil, fmt.Errorf("invalid duration %q", duration)
	}

	dest := destOperator(conn)
	var ops []*pb.Operator
	switch scope {
	case ScopeHost:
		ops = []*pb.Operator{dest}
	case ScopeProgram:
		ops = []*pb.Operator{simple(operandProcessPath, conn.ProcessPath)}
	case ScopeProgramHost:
		ops = []*pb.Operator{simple(operandProcessPath, conn.ProcessPath), dest}
	default:
		return nil, fmt.Errorf("invalid scope %q", scope)
	}
	for _, op := range ops {
		if op.Data == "" {
			return nil, fmt.Errorf("scope %q needs %s, which the connection lacks", scope, op.Operand)
		}
	}

	op := ops[0]
	kind := typeSimple
	if len(ops) > 1 {
		kind = typeList
		op = listOperator(ops)
	}

	parts := []string{action, duration, kind}
	for _, o := range ops {
		parts = append(parts, o.Data)
	}
	return &pb.Rule{
		Created:  now.Unix(),
		Name:     Slug(strings.Join(parts, " ")),
		Enabled:  true,
		Action:   action,
		Duration: duration,
		Operator: op,
	}, nil
}

// Fallback is the one-shot rule returned when nobody answered the prompt.
func Fallback(action string, conn *pb.Connection, now time.Time) *pb.Rule {
	scope := ScopeProgram
	if conn.ProcessPath == "" {
		scope = ScopeHost
	}
	r, err := Build(action, DurationOnce, scope, conn, now)
	if err != nil {
		// No process path and no destination: match on protocol so the
		// daemon still gets a valid rule.
		r = &pb.Rule{
			Created: now.Unix(), Name: Slug(action + " once fallback"), Enabled: true,
			Action: action, Duration: DurationOnce,
			Operator: &pb.Operator{Type: typeSimple, Operand: "protocol", Data: conn.Protocol},
		}
	}
	return r
}

func destOperator(conn *pb.Connection) *pb.Operator {
	if conn.DstHost != "" {
		return simple(operandDestHost, conn.DstHost)
	}
	return simple(operandDestIP, conn.DstIp)
}

func simple(operand, data string) *pb.Operator {
	return &pb.Operator{Type: typeSimple, Operand: operand, Data: data}
}

// listOperator mirrors the stock UI: the daemon reads List, while Data
// carries the same operators as JSON for display purposes.
func listOperator(ops []*pb.Operator) *pb.Operator {
	type jsonOp struct {
		Type    string `json:"type"`
		Operand string `json:"operand"`
		Data    string `json:"data"`
	}
	js := make([]jsonOp, len(ops))
	for i, o := range ops {
		js[i] = jsonOp{o.Type, o.Operand, o.Data}
	}
	data, _ := json.Marshal(js)
	return &pb.Operator{Type: typeList, Operand: typeList, Data: string(data), List: ops}
}

// Slug produces a filesystem-safe rule name; the daemon stores "always"
// rules as <name>.json.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if len(out) > 128 {
		out = strings.TrimSuffix(out[:128], "-")
	}
	return out
}
