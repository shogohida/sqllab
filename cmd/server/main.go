// Command server runs the sqllab demo: it builds the seed dataset once at
// startup, then serves the embedded frontend and the /api/* endpoints on a
// single port — the same free-tier-friendly shape used by the sibling
// raftkv project's demoserver.
package main

import (
	"log"
	"net/http"
	"os"

	"sqllab/internal/api"
	sqllabdb "sqllab/internal/db"
	"sqllab/internal/session"
	"sqllab/web"
)

func main() {
	tmpDir, err := os.MkdirTemp("", "sqllab-template")
	if err != nil {
		log.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log.Println("seeding demo dataset...")
	templatePath, err := sqllabdb.BuildTemplate(tmpDir)
	if err != nil {
		log.Fatalf("build seed dataset: %v", err)
	}
	log.Println("dataset ready")

	store := session.NewStore(templatePath)
	stop := make(chan struct{})
	defer close(stop)
	go store.RunEvictionLoop(stop)

	mux := http.NewServeMux()
	mux.Handle("/api/", api.New(store).Routes())
	mux.Handle("/", http.FileServer(http.FS(web.Assets)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("sqllab: serving on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server died: %v", err)
	}
}
