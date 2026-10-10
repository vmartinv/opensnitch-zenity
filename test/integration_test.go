// Package test drives the real server over a unix socket the way
// opensnitchd 1.7.2 does (daemon/ui/client.go, notifications.go).
package test

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/vmartinv/opensnitch-zenity/internal/prompt"
	"github.com/vmartinv/opensnitch-zenity/internal/rules"
	"github.com/vmartinv/opensnitch-zenity/internal/server"
	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

type fakePrompter struct {
	mu      sync.Mutex
	choice  prompt.Choice
	err     error
	delay   time.Duration
	calls   atomic.Int32
	active  atomic.Int32
	overlap atomic.Bool
}

func (f *fakePrompter) Prompt(ctx context.Context, _ *pb.Connection) (prompt.Choice, error) {
	f.calls.Add(1)
	if f.active.Add(1) > 1 {
		f.overlap.Store(true)
	}
	defer f.active.Add(-1)
	time.Sleep(f.delay)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.choice, f.err
}

type gate struct{ err error }

func (g gate) Usable(context.Context) error { return g.err }

func start(t *testing.T, p prompt.Prompter, g server.Gate) pb.UIClient {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "osui.sock")
	lis, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	pb.RegisterUIServer(srv, server.New(p, g, "deny", nil))
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("unix://"+sock, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewUIClient(conn)
}

func curl() *pb.Connection {
	return &pb.Connection{
		Protocol: "tcp", SrcIp: "192.168.1.10", SrcPort: 51000,
		DstIp: "93.184.216.34", DstHost: "example.com", DstPort: 443,
		UserId: 1000, ProcessId: 4242, ProcessPath: "/usr/bin/curl",
		ProcessArgs: []string{"curl", "https://example.com"},
	}
}

// validForDaemon checks what rule.Deserialize and the daemon's verdict code
// rely on: a non-nil operator, list items for list rules, and enabled=true
// (a disabled rule makes the daemon apply its own default action).
func validForDaemon(t *testing.T, r *pb.Rule) {
	t.Helper()
	if r == nil || r.Operator == nil {
		t.Fatalf("rule or operator is nil: %v", r)
	}
	if !r.Enabled {
		t.Error("rule is not enabled")
	}
	if r.Name == "" || r.Name != rules.Slug(r.Name) {
		t.Errorf("bad rule name %q", r.Name)
	}
	if r.Operator.Type == "list" && len(r.Operator.List) < 2 {
		t.Errorf("list operator without items: %v", r.Operator)
	}
}

func TestDaemonHandshake(t *testing.T) {
	c := start(t, &fakePrompter{}, gate{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &pb.ClientConfig{Name: "testhost", Version: "1.7.2", Config: `{"DefaultAction":"allow"}`}
	got, err := c.Subscribe(ctx, cfg)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got.Config != cfg.Config {
		t.Errorf("Subscribe must hand back the daemon config, got %q", got.Config)
	}

	stream, err := c.Notifications(ctx)
	if err != nil {
		t.Fatalf("Notifications: %v", err)
	}
	if err := stream.Send(&pb.NotificationReply{Code: pb.NotificationReplyCode_OK}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	recv := make(chan error, 1)
	go func() { _, err := stream.Recv(); recv <- err }()
	select {
	case err := <-recv:
		t.Fatalf("notifications stream closed (%v); the daemon would disconnect", err)
	case <-time.After(300 * time.Millisecond):
	}

	for _, id := range []uint64{1, 1791647601000000000} {
		pong, err := c.Ping(ctx, &pb.PingRequest{Id: id, Stats: &pb.Statistics{DaemonVersion: "1.7.2"}})
		if err != nil || pong.Id != id {
			t.Errorf("Ping(%d) = %v, %v", id, pong, err)
		}
	}

	if _, err := c.PostAlert(ctx, &pb.Alert{Id: 7, Type: pb.Alert_ERROR, Data: &pb.Alert_Text{Text: "eBPF failed"}}); err != nil {
		t.Errorf("PostAlert: %v", err)
	}
}

func TestAskRuleUserChoice(t *testing.T) {
	p := &fakePrompter{choice: prompt.Choice{Action: "allow", Duration: "always", Scope: rules.ScopeProgramHost}}
	c := start(t, p, gate{})
	r, err := c.AskRule(context.Background(), curl())
	if err != nil {
		t.Fatal(err)
	}
	validForDaemon(t, r)
	if r.Action != "allow" || r.Duration != "always" || r.Operator.Type != "list" {
		t.Errorf("rule = %v", r)
	}
}

func TestAskRuleFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		prompter   *fakePrompter
		gate       gate
		wantAction string
	}{
		{"dialog timed out", &fakePrompter{err: prompt.ErrTimeout}, gate{}, "deny"},
		{"zenity broken", &fakePrompter{err: errors.New("exec: not found")}, gate{}, "deny"},
		{"session locked", &fakePrompter{choice: prompt.Choice{Action: "allow", Duration: "always", Scope: rules.ScopeProgram}}, gate{errors.New("locked")}, "deny"},
		{"invalid choice", &fakePrompter{choice: prompt.Choice{Action: "allow", Duration: "forever", Scope: rules.ScopeProgram}}, gate{}, "deny"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := start(t, tt.prompter, tt.gate)
			r, err := c.AskRule(context.Background(), curl())
			if err != nil {
				t.Fatal(err)
			}
			validForDaemon(t, r)
			if r.Action != tt.wantAction || r.Duration != "once" {
				t.Errorf("rule = %s %s, want %s once", r.Action, r.Duration, tt.wantAction)
			}
		})
	}
	t.Run("locked session never prompts", func(t *testing.T) {
		p := &fakePrompter{}
		c := start(t, p, gate{errors.New("locked")})
		c.AskRule(context.Background(), curl())
		if p.calls.Load() != 0 {
			t.Error("prompted while locked")
		}
	})
}

func TestAskRuleDedupAndSerialization(t *testing.T) {
	p := &fakePrompter{
		choice: prompt.Choice{Action: "allow", Duration: "once", Scope: rules.ScopeProgramHost},
		delay:  100 * time.Millisecond,
	}
	c := start(t, p, gate{})

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		other := curl()
		other.DstPort = uint32(8000 + i)
		wg.Add(2)
		go func() { defer wg.Done(); c.AskRule(context.Background(), curl()) }()
		go func() { defer wg.Done(); c.AskRule(context.Background(), other) }()
	}
	wg.Wait()

	if p.overlap.Load() {
		t.Error("two dialogs were open at once")
	}
	// one dialog for the repeated connection plus one per distinct port
	if got := p.calls.Load(); got != 4 {
		t.Errorf("prompted %d times, want 4", got)
	}
}

func TestAskRuleDaemonGivesUp(t *testing.T) {
	p := &fakePrompter{err: prompt.ErrTimeout, delay: 300 * time.Millisecond}
	c := start(t, p, gate{})
	go c.AskRule(context.Background(), curl()) // occupies the dialog

	time.Sleep(50 * time.Millisecond)
	other := curl()
	other.DstHost = "other.example"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.AskRule(ctx, other); err == nil {
		t.Log("answered before the deadline")
	}
	if time.Since(start) > 250*time.Millisecond {
		t.Error("waiting AskRule did not respect the daemon's deadline")
	}
}
