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

	"github.com/pranav-k/forkrisk/internal/dashboard"
)

func main() {
	port := flag.Int("port", 8080, "HTTP server port (defaults to 8080 with fallback to 8081)")
	host := flag.String("host", "127.0.0.1", "HTTP server host binding")
	dataDir := flag.String("data-dir", "outputs", "Path to simulation and ML outputs directory")
	webDir := flag.String("web-dir", "web", "Path to static frontend web assets directory")
	flag.Parse()
	portSpecified := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portSpecified = true
		}
	})

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
	listener, err := net.Listen("tcp", server.Addr())
	if err != nil && !portSpecified {
		host, _, _ := net.SplitHostPort(server.Addr())
		alternate := net.JoinHostPort(host, "8081")
		var alternateErr error
		listener, alternateErr = net.Listen("tcp", alternate)
		if alternateErr != nil {
			log.Fatalf("Dashboard could not open either default port. Use --port to choose an available port.\n%v\n%v", err, alternateErr)
		}
		log.Printf("Dashboard could not open %s, using %s instead.", server.Addr(), listener.Addr())
		err = nil
	}
	if err != nil {
		log.Fatalf("Dashboard could not open %s. Use --port to choose an available port. %v", server.Addr(), err)
	}

	stop, cancelSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancelSignals()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	fmt.Printf("\n✓ ForkRisk Dashboard active at http://%s\n", listener.Addr())
	fmt.Println("✓ API endpoints available under /api/ (health, runs, scenarios, models, drift, sensitivity)")
	fmt.Println("✓ Press Ctrl+C to terminate.")

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Dashboard server failed: %v", err)
		}
		return
	case <-stop.Done():
	}
	fmt.Println("\nShutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	fmt.Println("Server gracefully stopped.")
}
