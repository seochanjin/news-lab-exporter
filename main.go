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

	// /healthz는 프로세스가 살아 있는지만 답한다. DB를 조회하지 않는다.
	//
	// 이유가 둘이다.
	// 1. /metrics 한 번이 DB 조회 7개를 유발하므로 probe가 감시 대상에 부하를 준다.
	// 2. DB 상태를 probe에 연동하면 DB 장애 때 Pod가 NotReady가 되어 Prometheus가
	//    scrape를 멈춘다. 그 순간이 바로 query_success=0이 필요한 때다.
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

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
