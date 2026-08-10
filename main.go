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

	db, err := openReadOnlyDB(cfg.DatabaseURL, cfg.DBMaxConns)
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

	newslabCollector := collector.New(db, cfg.ScrapeTimeout)

	prometheus.MustRegister(newslabCollector)

	buildInfo.Set(1)

	http.Handle(cfg.MetricsPath, promhttp.Handler())

	log.Printf("listening on %s", cfg.ListenAddr)

	err = http.ListenAndServe(cfg.ListenAddr, nil)
	if err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// openReadOnlyDB는 read-only 전용 DB 연결을 만든다.
//
// read-only는 쿼리마다 선언하지 않고 connection 파라미터로 강제한다.
// 커넥션 풀이 새 연결을 만들어도 자동 적용되므로 빠뜨릴 수 없다.
//
// Supabase transaction pooler(6543)는 트랜잭션마다 backend connection을 다시 배정한다.
// pgx 기본값인 extended protocol은 PREPARE한 backend와 EXECUTE하는 backend가 달라지면
// `prepared statement ... does not exist (SQLSTATE 26000)`으로 실패한다.
// 실제로 쿼리 7개를 동시 실행하자 매 scrape마다 무작위로 5~6개가 이 오류로 실패했다.
// 따라서 prepared statement를 만들지 않는 simple protocol을 사용한다.
//
// 커넥션 수는 상한을 둔다. 감시자가 감시 대상의 커넥션을 무제한으로 점유하면 안 된다.
func openReadOnlyDB(databaseURL string, maxConns int) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	connCfg.RuntimeParams["default_transaction_read_only"] = "on"
	connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	db := stdlib.OpenDB(*connCfg)
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

	return db, nil
}
