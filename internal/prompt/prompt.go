package prompt

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vmartinv/opensnitch-zenity/internal/rules"
	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

var ErrTimeout = errors.New("dialog timed out")

type Choice struct {
	Action   string
	Duration string
	Scope    rules.Scope
}

type Prompter interface {
	Prompt(ctx context.Context, conn *pb.Connection) (Choice, error)
}

// Zenity asks with up to two button dialogs: the decision, then, for
// "always" answers, what the rule should cover.
type Zenity struct {
	Path    string
	Timeout time.Duration // per dialog
}

const (
	btnAllowOnce   = "Allow once"
	btnDenyOnce    = "Deny once"
	btnAllowAlways = "Allow always"
	btnDenyAlways  = "Deny always"
	btnJustOnce    = "Just once"
)

func (z *Zenity) Prompt(ctx context.Context, conn *pb.Connection) (Choice, error) {
	pressed, err := z.run(ctx, decisionArgs(conn, z.Timeout))
	if err != nil {
		return Choice{}, err
	}
	c := Choice{Duration: rules.DurationOnce, Scope: rules.ScopeProgram}
	if conn.ProcessPath == "" {
		c.Scope = rules.ScopeHost
	}
	switch pressed {
	case btnAllowOnce:
		c.Action = rules.ActionAllow
	case btnDenyOnce:
		c.Action = rules.ActionDeny
	case btnAllowAlways:
		c.Action, c.Duration = rules.ActionAllow, rules.DurationAlways
	case btnDenyAlways:
		c.Action, c.Duration = rules.ActionDeny, rules.DurationAlways
	default:
		return Choice{}, fmt.Errorf("unexpected zenity answer %q", pressed)
	}
	if c.Duration == rules.DurationOnce || conn.ProcessPath == "" {
		return c, nil
	}

	opts := scopeOptions(conn)
	pressed, err = z.run(ctx, scopeArgs(conn, c.Action, opts, z.Timeout))
	if errors.Is(err, ErrTimeout) || pressed == btnJustOnce {
		c.Duration = rules.DurationOnce
		return c, nil
	}
	if err != nil {
		return Choice{}, err
	}
	for _, o := range opts {
		if o.label == pressed {
			c.Scope = o.scope
			return c, nil
		}
	}
	return Choice{}, fmt.Errorf("unexpected zenity answer %q", pressed)
}

// run shows a --question dialog and returns the label of the pressed button.
// OK is the first label, Cancel (also window close) the second.
func (z *Zenity) run(ctx context.Context, d dialog) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, z.Timeout+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, z.Path, d.args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ErrTimeout
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return d.ok, nil
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 5:
		return "", ErrTimeout
	case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
		// Extra buttons print their label; Cancel and close print nothing.
		if label := strings.TrimRight(string(out), "\n"); label != "" {
			return label, nil
		}
		return d.cancel, nil
	}
	return "", fmt.Errorf("running zenity: %w", err)
}

type dialog struct {
	args       []string
	ok, cancel string
}

func newDialog(text string, timeout time.Duration, ok, cancel string, extra ...string) dialog {
	args := []string{
		"--question",
		"--title=OpenSnitch",
		"--width=520",
		"--icon=network-wired",
		"--timeout=" + strconv.Itoa(int(timeout.Seconds())),
		"--text=" + text,
		"--ok-label=" + ok,
		"--cancel-label=" + cancel,
	}
	for _, e := range extra {
		args = append(args, "--extra-button="+e)
	}
	return dialog{args: args, ok: ok, cancel: cancel}
}

// Closing the window must be the safe answer, so Cancel is "Deny once".
func decisionArgs(conn *pb.Connection, timeout time.Duration) dialog {
	return newDialog(Text(conn), timeout, btnAllowOnce, btnDenyOnce, btnDenyAlways, btnAllowAlways)
}

type scopeOption struct {
	scope rules.Scope
	label string
}

func scopeOptions(conn *pb.Connection) []scopeOption {
	prog := clean(filepath.Base(conn.ProcessPath))
	dest := clean(conn.DstHost)
	if dest == "" {
		dest = clean(conn.DstIp)
	}
	return []scopeOption{
		{rules.ScopeProgram, prog + " to any host"},
		{rules.ScopeProgramHost, prog + " to " + dest + " only"},
		{rules.ScopeHost, "Any program to " + dest},
	}
}

func scopeArgs(conn *pb.Connection, action string, opts []scopeOption, timeout time.Duration) dialog {
	text := fmt.Sprintf("<b>%s always</b> for which connections?\n\n%s", capitalize(action), Text(conn))
	return newDialog(text, timeout, opts[0].label, btnJustOnce, opts[2].label, opts[1].label)
}

// Text renders the connection as Pango markup; every field is
// attacker-controlled and therefore escaped.
func Text(conn *pb.Connection) string {
	e := func(s string) string { return html.EscapeString(clean(s)) }
	dest := "<b>" + e(conn.DstIp) + "</b>"
	if conn.DstHost != "" {
		dest = fmt.Sprintf("<b>%s</b> (%s)", e(conn.DstHost), e(conn.DstIp))
	}
	prog := conn.ProcessPath
	if prog == "" {
		prog = "unknown process"
	}
	uname := strconv.FormatUint(uint64(conn.UserId), 10)
	if u, err := user.LookupId(uname); err == nil {
		uname = fmt.Sprintf("%s (%d)", u.Username, conn.UserId)
	}
	return fmt.Sprintf("<b>%s</b> wants to connect to\n%s port %d/%s\n\nCommand: %s\nUser: %s   PID: %d",
		e(prog), dest, conn.DstPort, e(conn.Protocol),
		e(truncate(strings.Join(conn.ProcessArgs, " "), 200)),
		e(uname), conn.ProcessId)
}

// clean drops control characters, which could fake extra lines in labels.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max-1]) + "…"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
