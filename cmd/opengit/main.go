// Command opengit runs the OpenGit server.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rxsuite/opengit/internal/mcp"
	"github.com/rxsuite/opengit/internal/server"
)

const version = "0.1.0"

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
		}
	}
	addr := flag.String("addr", "127.0.0.1:3000", "listen address")
	data := flag.String("data", "data", "data directory")
	noReg := flag.Bool("disable-registration", false, "block signups once the first (admin) user exists")
	flag.Parse()

	s, err := server.New(server.Config{DataDir: *data, DisableRegistration: *noReg})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Printf("OpenGit listening on http://%s (first account registered becomes admin)", *addr)
	srv := &http.Server{Addr: *addr, Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Fatal(srv.ListenAndServe())
}
