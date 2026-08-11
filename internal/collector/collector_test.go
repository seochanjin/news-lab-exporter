package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// --- 도우미 ------------------------------------------------------

// collectMetrics는 collect 함수를 실행하고 내보낸 지표를 모아 돌려준다.
func collectMetrics(collect func(context.Context, chan<- prometheus.Metric)) []prometheus.Metric {
	ch := make(chan prometheus.Metric, 64)
	collect(context.Background(), ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	return metrics
}

// findMetric은 지표 목록에서 이름이 일치하는 첫 지표를 찾는다. 없으면 nil.
func findMetric(metrics []prometheus.Metric, name string) prometheus.Metric {
	for _, m := range metrics {
		if strings.Contains(m.Desc().String(), `fqName: "`+name+`"`) {
			return m
		}
	}

	return nil
}

// countMetric은 이름이 일치하는 지표가 몇 개인지 센다.
func countMetric(metrics []prometheus.Metric, name string) int {
	count := 0
	for _, m := range metrics {
		if strings.Contains(m.Desc().String(), `fqName: "`+name+`"`) {
			count++
		}
	}

	return count
}

// valueOf는 지표의 실제 숫자를 읽는다.
func valueOf(t *testing.T, m prometheus.Metric) float64 {
	t.Helper()

	var pb dto.Metric

	err := m.Write(&pb)
	if err != nil {
		t.Fatalf("지표 값을 읽지 못했다: %v", err)
	}

	if pb.Counter != nil {
		return pb.Counter.GetValue()
	}
	if pb.Gauge != nil {
		return pb.Gauge.GetValue()
	}

	t.Fatal("counter도 gauge도 아닌 지표다")
	return 0
}

// --- 테스트 ------------------------------------------------------

func TestCollectCrawlRuns_정상이면_지표를_내보낸다(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock 생성 실패: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"collected", "skipped"}).AddRow(9613, 3892)
	mock.ExpectQuery("from crawl_runs").WillReturnRows(rows)

	c := New(db, time.Second)
	metrics := collectMetrics(c.collectCrawlRuns)

	collected := findMetric(metrics, "newslab_articles_collected_total")
	if collected == nil {
		t.Fatal("articles_collected_total이 없다")
	}

	got := valueOf(t, collected)
	if got != 9613 {
		t.Errorf("collected got=%v, want=9613", got)
	}

	success := findMetric(metrics, "newslab_exporter_query_success")
	if success == nil {
		t.Fatal("query_success가 없다")
	}

	if valueOf(t, success) != 1 {
		t.Error("query_success가 1이어야 한다")
	}
}

// 쿼리가 실패하면 업무 지표를 0으로 덮지 않고 아예 내보내지 않아야 한다.
// 2026-08-10 트랜잭션 풀러 장애에서 실제로 확인한 동작을 고정한다.
func TestCollectCrawlRuns_쿼리가_실패하면_업무지표를_내보내지_않는다(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock 생성 실패: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery("from crawl_runs").WillReturnError(errors.New("연결 끊김"))

	c := New(db, time.Second)
	metrics := collectMetrics(c.collectCrawlRuns)

	if findMetric(metrics, "newslab_articles_collected_total") != nil {
		t.Error("쿼리가 실패했는데 articles_collected_total이 노출됐다")
	}

	if findMetric(metrics, "newslab_articles_skipped_total") != nil {
		t.Error("쿼리가 실패했는데 articles_skipped_total이 노출됐다")
	}

	success := findMetric(metrics, "newslab_exporter_query_success")
	if success == nil {
		t.Fatal("query_success가 없다")
	}

	if valueOf(t, success) != 0 {
		t.Error("query_success가 0이어야 한다")
	}
}

// 한 번도 성공한 적 없는 파이프라인은 0을 내보내지 않고 지표를 생략해야 한다.
// 0을 내보내면 1970-01-01로 표시되어 "오래됨"과 구분할 수 없다.
func TestCollectPipelineLastSuccess_성공이력이_없으면_지표를_생략한다(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock 생성 실패: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"pipeline", "epoch"}).
		AddRow("three_day", 1786046774.0).
		AddRow("weekly", nil)

	mock.ExpectQuery("finished_at").WillReturnRows(rows)

	c := New(db, time.Second)
	metrics := collectMetrics(c.collectPipelineLastSuccess)

	got := countMetric(metrics, "newslab_pipeline_last_success_timestamp_seconds")
	if got != 1 {
		t.Errorf("NULL인 weekly는 생략돼야 한다. 지표 수 got=%d, want=1", got)
	}
}
