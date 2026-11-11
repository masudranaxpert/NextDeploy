package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

func main() {
	listen := flag.String("listen", envOr("LISTEN_ADDR", ":8080"), "HTTP listen address")
	originName := flag.String("name", envOr("ORIGIN_NAME", "origin-a"), "Origin identifier")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("ws upgrade error: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			reply := append([]byte(*originName+": "), msg...)
			if err := conn.WriteMessage(mt, reply); err != nil {
				return
			}
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"origin":          *originName,
			"host":            r.Host,
			"path":            r.URL.Path,
			"x_real_ip":       r.Header.Get("X-Real-IP"),
			"x_forwarded_for": r.Header.Get("X-Forwarded-For"),
		})
	})

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("origin %q listening on %s", *originName, *listen)
	log.Fatal(srv.ListenAndServe())
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
