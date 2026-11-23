// Command opengit runs the OpenGit server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rxsuite/opengit/internal/mcp"
	"github.com/rxsuite/opengit/internal/server"
)

const version = "0.1.0"

type options struct {
	Addr, Data, Cert, Key string
	NoReg                 bool
}

func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("opengit", flag.ContinueOnError)
	fs.StringVar(&o.Addr, "addr", "127.0.0.1:3000", "listen address (use 0.0.0.0:3000 to accept connections from your network)")
	fs.StringVar(&o.Data, "data", "data", "data directory (database + repositories)")
	fs.BoolVar(&o.NoReg, "disable-registration", false, "block signups once the first (admin) user exists")
	fs.StringVar(&o.Cert, "tls-cert", "", "TLS certificate file (PEM); enables HTTPS together with -tls-key")
	fs.StringVar(&o.Key, "tls-key", "", "TLS private key file (PEM)")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if (o.Cert == "") != (o.Key == "") {
		return o, errors.New("-tls-cert and -tls-key must be given together")
	}
	return o, nil
}

// serve runs the server until ctx is cancelled, then shuts down gracefully.
func serve(ctx context.Context, o options) error {
	s, err := server.New(server.Config{DataDir: o.Data, DisableRegistration: o.NoReg})
	if err != nil {
		return err
	}
	defer s.Close()
	srv := &http.Server{Addr: o.Addr, Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	scheme := "http"
	if o.Cert != "" {
		scheme = "https"
	}
	if host, _, err := net.SplitHostPort(o.Addr); err == nil && scheme == "http" {
		if ip := net.ParseIP(host); host == "" || (ip != nil && !ip.IsLoopback()) {
			log.Print("warning: listening on the network without TLS; passwords and tokens travel unencrypted. Use -tls-cert/-tls-key, a reverse proxy, or a VPN such as Tailscale.")
		}
	}
	log.Printf("OpenGit %s listening on %s://%s (data: %s)", version, scheme, o.Addr, o.Data)
	errc := make(chan error, 1)
	go func() {
		if o.Cert != "" {
			errc <- srv.ListenAndServeTLS(o.Cert, o.Key)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sc, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(sc)
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			// stdio MCP server for Claude Code. stdout is the protocol channel: log to stderr only.
			url := os.Getenv("OPENGIT_URL")
			if url == "" {
				url = "http://127.0.0.1:3000"
			}
			m := &mcp.Server{BaseURL: url, Token: os.Getenv("OPENGIT_TOKEN"), Version: version}
			if err := m.Serve(os.Stdin, os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "opengit mcp:", err)
				os.Exit(1)
			}
			return
		case "version", "--version":
			fmt.Println("opengit", version)
			return
		case "service":
			if err := manageService(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "opengit service:", err)
				os.Exit(1)
			}
			return
		}
	}
	o, err := parseOptions(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatal(err)
	}
	if ran, err := runAsService(o); ran { // started by the Windows service manager
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, o); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
