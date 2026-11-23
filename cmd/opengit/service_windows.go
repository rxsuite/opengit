//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const svcName = "OpenGit"

type handler struct{ o options }

func (h handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, h.o) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done: // server died on its own
			if err != nil {
				log.Print("server stopped: ", err)
				return false, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// runAsService runs under the Service Control Manager when launched by it.
func runAsService(o options) (bool, error) {
	is, err := svc.IsWindowsService()
	if err != nil || !is {
		return false, nil
	}
	if abs, err := filepath.Abs(o.Data); err == nil {
		o.Data = abs
	}
	os.MkdirAll(o.Data, 0o755)
	if f, err := os.OpenFile(filepath.Join(o.Data, "opengit.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(f)
	}
	return true, svc.Run(svcName, handler{o})
}

// manageService implements: opengit service install|uninstall|start|stop [server flags]
func manageService(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: opengit service install|uninstall|start|stop [-addr ... -data C:\\opengit\\data ...]")
	}
	cmd, rest := args[0], args[1:]
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("cannot talk to the service manager (run this from an Administrator terminal): %w", err)
	}
	defer m.Disconnect()
	switch cmd {
	case "install":
		o, err := parseOptions(rest)
		if err != nil {
			return err
		}
		if o.Data, err = filepath.Abs(o.Data); err != nil { // services start in System32: paths must be absolute
			return err
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		sargs := []string{"-addr", o.Addr, "-data", o.Data}
		if o.NoReg {
			sargs = append(sargs, "-disable-registration")
		}
		if o.Cert != "" {
			c, _ := filepath.Abs(o.Cert)
			k, _ := filepath.Abs(o.Key)
			sargs = append(sargs, "-tls-cert", c, "-tls-key", k)
		}
		s, err := m.CreateService(svcName, exe, mgr.Config{
			DisplayName: "OpenGit", Description: "Self-hosted git forge", StartType: mgr.StartAutomatic,
		}, sargs...)
		if err != nil {
			return err
		}
		defer s.Close()
		s.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 5 * time.Second}, {Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		}, 86400)
		if err := s.Start(); err != nil {
			return fmt.Errorf("installed, but could not start: %w", err)
		}
		fmt.Println("OpenGit service installed and started. Data:", o.Data)
		return nil
	case "uninstall", "start", "stop":
		s, err := m.OpenService(svcName)
		if err != nil {
			return fmt.Errorf("service %q not found: %w", svcName, err)
		}
		defer s.Close()
		switch cmd {
		case "start":
			return s.Start()
		case "stop":
			_, err := s.Control(svc.Stop)
			return err
		default:
			s.Control(svc.Stop)
			return s.Delete()
		}
	}
	return fmt.Errorf("unknown command %q", cmd)
}
