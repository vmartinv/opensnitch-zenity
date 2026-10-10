// Package server implements the protocol.UI gRPC service that opensnitchd
// connects to.
package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/vmartinv/opensnitch-zenity/internal/log"
	"github.com/vmartinv/opensnitch-zenity/internal/prompt"
	"github.com/vmartinv/opensnitch-zenity/internal/rules"
	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

// Answers for an identical connection are reused for this long, so a burst
// of retries from one program produces one dialog.
const DedupWindow = 2 * time.Second

type Gate interface {
	Usable(ctx context.Context) error
}

type Server struct {
	pb.UnimplementedUIServer

	prompter      prompt.Prompter
	gate          Gate
	defaultAction string
	notify        func(summary, body string)
	now           func() time.Time

	turn chan struct{} // one dialog at a time

	mu     sync.Mutex
	recent map[string]cached
}

type cached struct {
	rule    *pb.Rule
	expires time.Time
}

// New returns a server. notify may be nil.
func New(p prompt.Prompter, gate Gate, defaultAction string, notify func(summary, body string)) *Server {
	return &Server{
		prompter:      p,
		gate:          gate,
		defaultAction: defaultAction,
		notify:        notify,
		now:           time.Now,
		turn:          make(chan struct{}, 1),
		recent:        map[string]cached{},
	}
}

func (s *Server) Ping(_ context.Context, req *pb.PingRequest) (*pb.PingReply, error) {
	return &pb.PingReply{Id: req.Id}, nil
}

func (s *Server) Subscribe(_ context.Context, cfg *pb.ClientConfig) (*pb.ClientConfig, error) {
	log.Info("Daemon %s %s subscribed", cfg.Name, cfg.Version)
	// The daemon reads DefaultAction back out of the returned config.
	return cfg, nil
}

// Notifications must stay open: the daemon treats a closed stream as the UI
// going away and reconnects.
func (s *Server) Notifications(stream grpc.BidiStreamingServer[pb.NotificationReply, pb.Notification]) error {
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				return
			}
		}
	}()
	<-stream.Context().Done()
	return nil
}

func (s *Server) PostAlert(_ context.Context, a *pb.Alert) (*pb.MsgResponse, error) {
	if a.Type != pb.Alert_INFO && a.GetText() != "" {
		log.Warn("Daemon alert (%s/%s): %s", a.Type, a.What, a.GetText())
	}
	return &pb.MsgResponse{Id: a.Id}, nil
}

func (s *Server) AskRule(ctx context.Context, conn *pb.Connection) (*pb.Rule, error) {
	key := dedupKey(conn)
	if r := s.cachedRule(key); r != nil {
		log.Debug("Reusing recent answer for %s", describe(conn))
		return r, nil
	}
	if err := s.gate.Usable(ctx); err != nil {
		return s.fallback(conn, err.Error()), nil
	}

	select {
	case s.turn <- struct{}{}:
		defer func() { <-s.turn }()
	case <-ctx.Done():
		return s.fallback(conn, "daemon gave up waiting"), nil
	}
	if r := s.cachedRule(key); r != nil {
		return r, nil
	}

	choice, err := s.prompter.Prompt(ctx, conn)
	if err != nil {
		return s.fallback(conn, err.Error()), nil
	}
	rule, err := rules.Build(choice.Action, choice.Duration, choice.Scope, conn, s.now())
	if err != nil {
		return s.fallback(conn, err.Error()), nil
	}

	log.Info("User chose %s %s (%s) for %s", rule.Action, rule.Duration, choice.Scope, describe(conn))
	s.remember(key, rule)
	return rule, nil
}

func (s *Server) fallback(conn *pb.Connection, reason string) *pb.Rule {
	log.Info("Applying default %s once to %s: %s", s.defaultAction, describe(conn), reason)
	if s.notify != nil {
		s.notify("OpenSnitch: "+s.defaultAction, describe(conn)+"\n"+reason)
	}
	return rules.Fallback(s.defaultAction, conn, s.now())
}

func (s *Server) cachedRule(key string) *pb.Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.recent[key]
	if !ok {
		return nil
	}
	if s.now().After(c.expires) {
		delete(s.recent, key)
		return nil
	}
	return proto.Clone(c.rule).(*pb.Rule)
}

func (s *Server) remember(key string, r *pb.Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, c := range s.recent {
		if now.After(c.expires) {
			delete(s.recent, k)
		}
	}
	s.recent[key] = cached{rule: proto.Clone(r).(*pb.Rule), expires: now.Add(DedupWindow)}
}

func dedupKey(c *pb.Connection) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%d\x00%s", c.UserId, c.ProcessPath, c.DstHost, c.DstIp, c.DstPort, c.Protocol)
}

func describe(c *pb.Connection) string {
	dest := c.DstIp
	if c.DstHost != "" {
		dest = c.DstHost
	}
	return fmt.Sprintf("%s -> %s:%d/%s", c.ProcessPath, dest, c.DstPort, c.Protocol)
}
