package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/vmartinv/opensnitch-zenity/internal/config"
	"github.com/vmartinv/opensnitch-zenity/internal/log"
	"github.com/vmartinv/opensnitch-zenity/internal/prompt"
	"github.com/vmartinv/opensnitch-zenity/internal/server"
	"github.com/vmartinv/opensnitch-zenity/internal/session"
	pb "github.com/vmartinv/opensnitch-zenity/proto"
)

func main() {
	configPath := flag.String("config", "", "config file (default "+config.DefaultPath()+")")
	socket := flag.String("socket", "", "listen address, unix:///path")
	timeout := flag.Duration("timeout", 0, "how long each dialog waits for an answer")
	defaultAction := flag.String("default-action", "", "action when nobody answers: deny, allow or reject")
	logLevel := flag.String("log-level", "", "debug, info, warn or error")
	flag.Parse()

	path, explicit := *configPath, *configPath != ""
	if !explicit {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path, explicit)
	if err != nil {
		fatal(err)
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "socket":
			cfg.Socket = *socket
		case "timeout":
			cfg.PromptTimeout = *timeout
		case "default-action":
			cfg.DefaultAction = *defaultAction
		case "log-level":
			cfg.LogLevel = *logLevel
		}
	})
	if err := cfg.Validate(); err != nil {
		fatal(err)
	}
	log.SetLevel(cfg.LogLevel)

	var notify func(string, string)
	if cfg.NotifyOnDefault {
		notify = func(summary, body string) {
			cmd := exec.Command("notify-send", "--app-name=OpenSnitch", summary, body)
			if cmd.Start() == nil {
				go cmd.Wait()
			}
		}
	}

	srv := server.New(
		&prompt.Zenity{Path: cfg.ZenityPath, Timeout: cfg.PromptTimeout},
		&session.Checker{LockedCommand: cfg.LockedCheckCommand},
		cfg.DefaultAction,
		notify,
	)

	lis, err := listen(strings.TrimPrefix(cfg.Socket, "unix://"))
	if err != nil {
		fatal(err)
	}
	g := grpc.NewServer()
	pb.RegisterUIServer(g, srv)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		log.Info("Shutting down")
		g.Stop() // closes the listener, which unlinks the socket
	}()

	log.Info("Listening on %s", cfg.Socket)
	if err := g.Serve(lis); err != nil {
		fatal(err)
	}
}

// listen creates the socket readable only by us; opensnitchd runs as root.
func listen(path string) (net.Listener, error) {
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, fmt.Errorf("another UI is already listening on %s", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("removing stale socket: %w", err)
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	return net.Listen("unix", path)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "opensnitch-zenity:", err)
	os.Exit(1)
}
