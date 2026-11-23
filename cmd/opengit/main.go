// Command opengit runs the OpenGit server.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/rxsuite/opengit/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:3000", "listen address")
	data := flag.String("data", "data", "data directory")
	flag.Parse()

	s, err := server.New(server.Config{
		DataDir: *data,
		Token:   os.Getenv("OPENGIT_TOKEN"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if os.Getenv("OPENGIT_TOKEN") == "" {
		log.Print("warning: OPENGIT_TOKEN not set; anyone who can reach this server can create repos and push")
	}
	log.Printf("OpenGit listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, s))
}
