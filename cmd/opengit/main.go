// Command opengit runs the OpenGit server.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/rxsuite/opengit/internal/server"
)

func main() {
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
