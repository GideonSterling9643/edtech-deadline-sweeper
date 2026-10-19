package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	sweep "github.com/infrai-fyi/edtech-deadline-sweeper"
)

type config struct {
	Bucket      string
	RecordsFile string
	Retention   time.Duration
	Client      *sweep.Client
}

func main() {
	flag.Parse()
	command := "serve"
	if flag.NArg() > 0 {
		command = flag.Arg(0)
	}
	if command == "decide" {
		must(runDecision(recordsPath()))
		return
	}
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := &sweep.Client{BaseURL: sweep.DefaultBaseURL, APIKey: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
	cfg := config{Bucket: env("EDTECH_BUCKET", "edtech-course-deliveries"), RecordsFile: recordsPath(), Retention: 90 * 24 * time.Hour, Client: client}

	switch command {
	case "install":
		must(install(context.Background(), cfg))
	case "serve":
		must(serve(cfg))
	default:
		log.Fatalf("unknown command %q; use serve, install, or decide", command)
	}
}

func install(ctx context.Context, cfg config) error {
	if err := cfg.Client.CreateBucket(ctx, cfg.Bucket); err != nil {
		return err
	}
	task := os.Getenv("SWEEP_TASK_URL")
	if task == "" {
		return errors.New("SWEEP_TASK_URL is required for install")
	}
	jobID, err := cfg.Client.CreateCron(ctx, "15 2 * * *", task)
	if err != nil {
		return err
	}
	fmt.Printf("scheduled job %s; bucket %s is ready\n", jobID, cfg.Bucket)
	return nil
}

func serve(cfg config) error {
	if err := cfg.Client.CreateBucket(context.Background(), cfg.Bucket); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sweep", cfg.handleSweep)
	addr := ":" + env("PORT", "8080")
	log.Printf("edtech cleanup listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func (cfg config) handleSweep(w http.ResponseWriter, r *http.Request) {
	deliveries, err := sweep.LoadDeliveries(cfg.RecordsFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	candidates := sweep.SelectStale(deliveries, time.Now().UTC(), cfg.Retention)
	keys := make([]string, len(candidates))
	for i := range candidates {
		keys[i] = candidates[i].ObjectKey
	}
	if len(keys) > 0 {
		if err := cfg.Client.DeleteBatch(r.Context(), cfg.Bucket, keys); err != nil {
			status := http.StatusBadGateway
			var apiErr *sweep.InfraiError
			if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
				status = apiErr.Status
			}
			http.Error(w, err.Error(), status)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sweep.Report{Scanned: learnerCount(deliveries), Deleted: len(candidates), Records: candidates})
}

func runDecision(path string) error {
	deliveries, err := sweep.LoadDeliveries(path)
	if err != nil {
		return err
	}
	result := sweep.SelectStale(deliveries, time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC), 90*24*time.Hour)
	return json.NewEncoder(os.Stdout).Encode(result)
}

func learnerCount(deliveries []sweep.CourseDelivery) int {
	total := 0
	for _, delivery := range deliveries {
		total += len(delivery.Learners)
	}
	return total
}

func recordsPath() string { return env("EDTECH_RECORDS_FILE", "example/deliveries.json") }

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
