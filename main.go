package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/seochanjin/news-lab-exporter/internal/collector"
	"github.com/seochanjin/news-lab-exporter/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	db, err := openReadOnlyDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open failed: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		log.Fatalf("db ping failed: %v", err)
	}
	log.Println("db connected (read only)")

	buildInfo := promauto.NewGauge(prometheus.GaugeOpts{
		Name: "newslab_exporter_build_info",
		Help: "Exporter build info. Always 1.",
	})

	newslabCollector := collector.New(db)

	prometheus.MustRegister(newslabCollector)

	buildInfo.Set(1)

	http.Handle(cfg.MetricsPath, promhttp.Handler())

	log.Printf("listening on %s", cfg.ListenAddr)

	err = http.ListenAndServe(cfg.ListenAddr, nil)
	if err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// openReadOnlyDB는 모든 connection이 read-only 트랜잭션을 쓰도록 강제해서 DB를 연다.
func openReadOnlyDB(databaseURL string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	connCfg.RuntimeParams["default_transaction_read_only"] = "on"

	db := stdlib.OpenDB(*connCfg)
	return db, nil
}
