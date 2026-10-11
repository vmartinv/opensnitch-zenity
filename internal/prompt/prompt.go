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
	Timeout time.Duration // for both dialogs together
}

// deadlineMargin keeps the last dialog inside the daemon's own deadline,
// leaving time to kill zenity and send the answer.
const deadlineMargin = 3 * time.Second

const (
	btnAllowOnce   = "Allow once"
	btnDenyOnce    = "Deny once"
	btnAllowAlways = "Allow always"
	btnDenyAlways  = "Deny always"
	btnBack        = "Back"
)

func (z *Zenity) Prompt(ctx context.Context, conn *pb.Connection) (Choice, error) {
	// One budget for both dialogs, so time left after the decision goes to
	// the scope dialog instead of being cut short.
	deadline := time.Now().Add(z.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Add(-deadlineMargin).Before(deadline) {
		deadline = d.Add(-deadlineMargin)
	}
	for {
		c, again, err := z.ask(ctx, conn, deadline)
		if !again {
			return c, err
		}
	}
}

// ask shows the decision dialog and, for "always", the scope dialog.
// again is true when the user pressed Back in the scope dialog.
func (z *Zenity) ask(ctx context.Context, conn *pb.Connection, deadline time.Time) (c Choice, again bool, err error) {
	if time.Until(deadline) <= 0 {
		return Choice{}, false, ErrTimeout
	}
	pressed, err := z.run(ctx, decisionArgs(conn, time.Until(deadline)))
	if err != nil {
		return Choice{}, false, err
	}
	c = Choice{Duration: rules.DurationOnce, Scope: rules.ScopeProgram}
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
		return Choice{}, false, fmt.Errorf("unexpected zenity answer %q", pressed)
	}
	if c.Duration == rules.DurationOnce || conn.ProcessPath == "" {
		return c, false, nil
	}

	// Running out of time here still applies the chosen action, once.
	c.Duration = rules.DurationOnce
	if time.Until(deadline) <= 0 {
		return c, false, nil
	}
	opts := scopeOptions(conn)
	pressed, err = z.run(ctx, scopeArgs(conn, c.Action, opts, time.Until(deadline)))
	if errors.Is(err, ErrTimeout) {
		return c, false, nil
	}
	if err != nil {
		return Choice{}, false, err
	}
	if pressed == btnBack {
		return Choice{}, true, nil
	}
	for _, o := range opts {
		if o.label == pressed {
			c.Duration, c.Scope = rules.DurationAlways, o.scope
			return c, false, nil
		}
	}
	return Choice{}, false, fmt.Errorf("unexpected zenity answer %q", pressed)
}

// run shows a --question dialog and returns the label of the pressed button.
// OK is the first label, Cancel (also window close) the second.
func (z *Zenity) run(ctx context.Context, d dialog) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout+2*time.Second)
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
	timeout    time.Duration
}

func newDialog(text string, timeout time.Duration, ok, cancel string, extra ...string) dialog {
	// zenity takes whole seconds and treats 0 as no timeout.
	timeout = max(timeout.Round(time.Second), time.Second)
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
	return dialog{args: args, ok: ok, cancel: cancel, timeout: timeout}
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
	// Most specific first; this is the order the user sees.
	return []scopeOption{
		{rules.ScopeProgramHost, prog + " to " + dest + " only"},
		{rules.ScopeProgram, prog + " to any host"},
		{rules.ScopeHost, "Any program to " + dest},
	}
}

func scopeArgs(conn *pb.Connection, action string, opts []scopeOption, timeout time.Duration) dialog {
	text := fmt.Sprintf("<b>%s always</b> for which connections?\n\n%s", capitalize(action), Text(conn))
	// Stacked zenity buttons show the extras reversed, then OK, then Cancel.
	last := len(opts) - 1
	extra := make([]string, 0, last)
	for i := last - 1; i >= 0; i-- {
		extra = append(extra, opts[i].label)
	}
	d := newDialog(text, timeout, opts[last].label, btnBack, extra...)
	// Enter must not pick the broadest scope (OK), so it goes Back.
	d.args = append(d.args, "--default-cancel")
	return d
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
