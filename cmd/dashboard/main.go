// Command dashboard runs the pure Go lightweight HTTP web server for the
// interactive ForkRisk dashboard per Milestone 6 of CASE_STUDY_DEMO_PLAN.md.
//
// Usage:
//
//	go run ./cmd/dashboard
//	go run ./cmd/dashboard --port 8080 --host 127.0.0.1 --data-dir outputs --web-dir web
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pranav-k/forkrisk/internal/dashboard"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port")
	host := flag.String("host", "127.0.0.1", "HTTP server host binding")
	dataDir := flag.String("data-dir", "outputs", "Path to simulation and ML outputs directory")
	webDir := flag.String("web-dir", "web", "Path to static frontend web assets directory")
	flag.Parse()

	fmt.Println("================================================================================")
	fmt.Println(" ForkRisk — Interactive Web Dashboard (Milestone 6)")
	fmt.Println(" Predicting Network-Induced Fork Risk in Blockchain Networks")
	fmt.Println("================================================================================")

	cfg := dashboard.ServerConfig{
		Host:    *host,
		Port:    *port,
		DataDir: *dataDir,
		WebDir:  *webDir,
	}

	server := dashboard.NewServer(cfg)

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("Server startup failed: %v", err)
		}
	}()

	fmt.Printf("\n✓ ForkRisk Dashboard active at http://%s:%d\n", *host, *port)
	fmt.Println("✓ API endpoints available under /api/ (health, runs, scenarios, models, drift, sensitivity)")
	fmt.Println("✓ Press Ctrl+C to terminate.")

	<-stop
	fmt.Println("\nShutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	fmt.Println("Server gracefully stopped.")
}
