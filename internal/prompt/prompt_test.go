package prompt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vmartinv/opensnitch-zenity/internal/rules"
	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

func conn() *pb.Connection {
	return &pb.Connection{
		Protocol: "tcp", DstIp: "93.184.216.34", DstHost: "example.com", DstPort: 443,
		UserId: 0, ProcessId: 42, ProcessPath: "/usr/bin/curl", ProcessArgs: []string{"curl", "https://example.com"},
	}
}

func TestTextEscapesMarkup(t *testing.T) {
	c := conn()
	c.ProcessPath = `/tmp/<span foreground="red">x</span>&`
	c.DstHost = "<b>evil</b>"
	c.ProcessArgs = []string{"a'b\"c", "\x1b[31m"}
	text := Text(c)
	for _, bad := range []string{"<span", "<b>evil", "a'b", "\x1b"} {
		if strings.Contains(text, bad) {
			t.Errorf("text contains unescaped %q:\n%s", bad, text)
		}
	}
	for _, good := range []string{"&lt;span", "&amp;", "&lt;b&gt;evil", "root (0)"} {
		if !strings.Contains(text, good) {
			t.Errorf("text lacks %q:\n%s", good, text)
		}
	}
}

func TestTextTruncatesArgsByRune(t *testing.T) {
	c := conn()
	c.ProcessArgs = []string{strings.Repeat("é", 500)}
	if n := strings.Count(Text(c), "é"); n != 199 {
		t.Errorf("kept %d runes of args, want 199", n)
	}
}

func TestDecisionDialogClosingDenies(t *testing.T) {
	d := decisionArgs(conn(), 30*time.Second)
	args := strings.Join(d.args, "\n")
	for _, want := range []string{"--question", "--timeout=30", "--cancel-label=Deny once", "--ok-label=Allow once",
		"--extra-button=Allow always", "--extra-button=Deny always"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q", want)
		}
	}
}

func TestScopeLabelsAreDistinctAndClean(t *testing.T) {
	c := conn()
	c.ProcessPath = "/tmp/Any program\nfake"
	c.DstHost = ""
	opts := scopeOptions(c)
	seen := map[string]bool{}
	for _, o := range opts {
		if strings.ContainsAny(o.label, "\n\r") || seen[o.label] {
			t.Errorf("bad label %q", o.label)
		}
		seen[o.label] = true
	}
	if l := labelFor(opts, rules.ScopeHost); !strings.Contains(l, "93.184.216.34") {
		t.Errorf("host label should fall back to IP: %q", l)
	}
}

// fakeZenity answers successive invocations with the given shell snippets
// and logs each invocation's arguments.
func fakeZenity(t *testing.T, answers ...string) (path, logFile string) {
	dir := t.TempDir()
	logFile = filepath.Join(dir, "calls")
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	counter := filepath.Join(dir, "count")
	fmt.Fprintf(&script, "printf '%%s\\n' \"$*\" >> '%s'\n", logFile)
	fmt.Fprintf(&script, "echo >> '%s'; n=$(wc -l < '%s')\n", counter, counter)
	for i, a := range answers {
		fmt.Fprintf(&script, "if [ \"$n\" = %d ]; then %s; fi\n", i+1, a)
	}
	script.WriteString("exit 99\n")
	path = filepath.Join(dir, "zenity")
	if err := os.WriteFile(path, []byte(script.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, logFile
}

const (
	pressOK     = "exit 0"
	pressCancel = "exit 1"
	timeout     = "exit 5"
)

func press(label string) string { return fmt.Sprintf("echo '%s'; exit 1", label) }

func labelFor(opts []scopeOption, s rules.Scope) string {
	for _, o := range opts {
		if o.scope == s {
			return o.label
		}
	}
	return ""
}

func TestPrompt(t *testing.T) {
	opts := scopeOptions(conn())
	tests := []struct {
		name    string
		answers []string
		want    Choice
		dialogs int
	}{
		{"allow once", []string{pressOK}, Choice{"allow", "once", rules.ScopeProgram}, 1},
		{"deny once", []string{pressCancel}, Choice{"deny", "once", rules.ScopeProgram}, 1},
		{"allow always, any host", []string{press(btnAllowAlways), press(labelFor(opts, rules.ScopeProgram))}, Choice{"allow", "always", rules.ScopeProgram}, 2},
		{"allow always, this host", []string{press(btnAllowAlways), press(labelFor(opts, rules.ScopeProgramHost))}, Choice{"allow", "always", rules.ScopeProgramHost}, 2},
		{"deny always, any program (OK button)", []string{press(btnDenyAlways), pressOK}, Choice{"deny", "always", rules.ScopeHost}, 2},
		{"always then just once", []string{press(btnDenyAlways), pressCancel}, Choice{"deny", "once", rules.ScopeProgram}, 2},
		{"always then scope timeout", []string{press(btnAllowAlways), timeout}, Choice{"allow", "once", rules.ScopeProgram}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, calls := fakeZenity(t, tt.answers...)
			z := &Zenity{Path: path, Timeout: time.Second}
			got, err := z.Prompt(context.Background(), conn())
			if err != nil || got != tt.want {
				t.Fatalf("got %v, %v; want %v", got, err, tt.want)
			}
			log, _ := os.ReadFile(calls)
			if n := strings.Count(string(log), "--question"); n != tt.dialogs {
				t.Errorf("showed %d dialogs, want %d", n, tt.dialogs)
			}
		})
	}
}

func TestPromptWithoutProcessSkipsScope(t *testing.T) {
	c := conn()
	c.ProcessPath = ""
	path, _ := fakeZenity(t, press(btnAllowAlways))
	got, err := (&Zenity{Path: path, Timeout: time.Second}).Prompt(context.Background(), c)
	want := Choice{"allow", "always", rules.ScopeHost}
	if err != nil || got != want {
		t.Errorf("got %v, %v; want %v", got, err, want)
	}
}

func TestPromptErrors(t *testing.T) {
	tests := []struct {
		name    string
		answers []string
		timeout time.Duration
		isTO    bool
	}{
		{"zenity timeout", []string{"echo junk; exit 5"}, time.Second, true},
		{"hung zenity", []string{"sleep 10"}, 0, true},
		{"unknown button", []string{press("Something else")}, time.Second, false},
		{"unknown scope", []string{press(btnAllowAlways), press("nope")}, time.Second, false},
		{"zenity crash", []string{"exit 255"}, time.Second, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := fakeZenity(t, tt.answers...)
			_, err := (&Zenity{Path: path, Timeout: tt.timeout}).Prompt(context.Background(), conn())
			if err == nil || errors.Is(err, ErrTimeout) != tt.isTO {
				t.Errorf("err = %v, want timeout=%v", err, tt.isTO)
			}
		})
	}
	if _, err := (&Zenity{Path: "/nonexistent/zenity", Timeout: time.Second}).Prompt(context.Background(), conn()); err == nil {
		t.Error("missing zenity should fail")
	}
}
