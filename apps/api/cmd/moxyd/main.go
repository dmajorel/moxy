// Command moxyd is the moxy aggregating backend: it queries N Proxmox VE
// clusters and exposes a unified API to the frontend, without ever handing API
// tokens to the browser.
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/dmajorel/moxy/apps/api/internal/server"
)

func main() {
	addr := flag.String("addr", defaultAddr(), "HTTP listen address")
	flag.Parse()

	srv := server.New(server.Options{Addr: *addr})

	log.Printf("moxyd listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("moxyd stopped: %v", err)
	}
}

func defaultAddr() string {
	if addr := os.Getenv("MOXY_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:8080"
}
