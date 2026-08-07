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

// serveJapanese serves the Japanese frontend at the friendly /ja path; the
// file itself lives at web/index.ja.html in the embedded FS so the plain
// http.FileServer above can also reach it directly at /index.ja.html.
func serveJapanese(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, web.Assets, "index.ja.html")
}

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
	mux.HandleFunc("GET /ja", serveJapanese)
	mux.HandleFunc("GET /ja/", serveJapanese)
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
