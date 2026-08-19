package collector

import (
	"context"
	"database/sql"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// NewsLabCollector는 NewsLab DB를 읽어 Prometheus 지표로 변환한다.
type NewsLabCollector struct {
	db            *sql.DB
	scrapeTimeout time.Duration

	articlesCollected *prometheus.Desc
	articlesSkipped   *prometheus.Desc
	articlesStored    *prometheus.Desc
	embeddingsStored  *prometheus.Desc

	pipelineRuns        *prometheus.Desc
	pipelineLastSuccess *prometheus.Desc

	embeddingCandidates *prometheus.Desc
	embeddingReused     *prometheus.Desc
	embeddingMissing    *prometheus.Desc

	topicsSelected *prometheus.Desc
	topicsSaved    *prometheus.Desc
	topicsFailed   *prometheus.Desc

	itemsProcessed *prometheus.Desc
	itemsFailed    *prometheus.Desc

	querySuccess *prometheus.Desc
}

// New는 collector를 만들고 지표 설계도를 미리 준비한다.
func New(db *sql.DB, scrapeTimeout time.Duration) *NewsLabCollector {
	return &NewsLabCollector{
		db:            db,
		scrapeTimeout: scrapeTimeout,

		articlesCollected: prometheus.NewDesc(
			"newslab_articles_collected_total",
			"RSS 수집 파이프라인이 저장한 기사 누적 건수.",
			nil, nil,
		),
		articlesSkipped: prometheus.NewDesc(
			"newslab_articles_skipped_total",
			"중복 등으로 저장하지 않은 기사 누적 건수.",
			nil, nil,
		),
		pipelineRuns: prometheus.NewDesc(
			"newslab_pipeline_runs_total",
			"파이프라인 실행 누적 횟수. status로 구분한다.",
			[]string{"pipeline", "status"}, nil,
		),
		querySuccess: prometheus.NewDesc(
			"newslab_exporter_query_success",
			"쿼리 성공 여부. 1이면 성공, 0이면 실패.",
			[]string{"query"}, nil,
		),
		articlesStored: prometheus.NewDesc(
			"newslab_articles_stored",
			"현재 저장된 기사 수. 삭제되면 감소한다.",
			nil, nil,
		),
		embeddingsStored: prometheus.NewDesc(
			"newslab_article_embeddings_stored",
			"현재 저장된 기사 임베딩 수. daily pipeline이 생성한 누적량.",
			nil, nil,
		),
		pipelineLastSuccess: prometheus.NewDesc(
			"newslab_pipeline_last_success_timestamp_seconds",
			"파이프라인이 마지막으로 success 상태로 끝난 시각(Unix epoch).",
			[]string{"pipeline"}, nil,
		),
		embeddingCandidates: prometheus.NewDesc(
			"newslab_pipeline_embedding_candidates_total",
			"토픽 선정 후보로 검토된 기사 누적 건수.",
			[]string{"pipeline"}, nil,
		),
		embeddingReused: prometheus.NewDesc(
			"newslab_pipeline_embedding_reused_total",
			"기존 임베딩을 재사용한 후보 누적 건수.",
			[]string{"pipeline"}, nil,
		),
		embeddingMissing: prometheus.NewDesc(
			"newslab_pipeline_embedding_missing_total",
			"임베딩이 없어 사용하지 못한 후보 누적 건수.",
			[]string{"pipeline"}, nil,
		),
		topicsSelected: prometheus.NewDesc(
			"newslab_pipeline_topics_selected_total",
			"저장 대상으로 선정된 토픽 누적 수.",
			[]string{"pipeline"}, nil,
		),
		topicsSaved: prometheus.NewDesc(
			"newslab_pipeline_topics_saved_total",
			"저장에 성공한 토픽 누적 수.",
			[]string{"pipeline"}, nil,
		),
		topicsFailed: prometheus.NewDesc(
			"newslab_pipeline_topics_failed_total",
			"저장에 실패한 토픽 누적 수.",
			[]string{"pipeline"}, nil,
		),
		itemsProcessed: prometheus.NewDesc(
			"newslab_pipeline_items_processed_total",
			"파이프라인이 처리에 성공한 항목 누적 수.",
			[]string{"pipeline"}, nil,
		),
		itemsFailed: prometheus.NewDesc(
			"newslab_pipeline_items_failed_total",
			"파이프라인이 처리에 실패한 항목 누적 수. run status로는 드러나지 않는다.",
			[]string{"pipeline"}, nil,
		),
	}
}

// Describe는 이 collector가 내보낼 지표의 설계도를 Prometheus에 알린다.
func (c *NewsLabCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.articlesCollected
	ch <- c.articlesSkipped
	ch <- c.articlesStored
	ch <- c.embeddingsStored
	ch <- c.pipelineRuns
	ch <- c.pipelineLastSuccess
	ch <- c.embeddingCandidates
	ch <- c.embeddingReused
	ch <- c.embeddingMissing
	ch <- c.topicsSelected
	ch <- c.topicsSaved
	ch <- c.topicsFailed
	ch <- c.itemsProcessed
	ch <- c.itemsFailed
	ch <- c.querySuccess
}

// Collect는 scrape 요청마다 호출된다.
// 쿼리 7개를 동시에 실행하되, 하나가 실패해도 나머지 지표는 그대로 내보낸다.
func (c *NewsLabCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.scrapeTimeout)
	defer cancel()

	collectors := []func(context.Context, chan<- prometheus.Metric){
		c.collectCrawlRuns,
		c.collectStoredCounts,
		c.collectPipelineRuns,
		c.collectPipelineLastSuccess,
		c.collectPipelineEmbeddings,
		c.collectPipelineTopics,
		c.collectExtractionItems,
	}

	var wg sync.WaitGroup

	for _, collect := range collectors {
		wg.Add(1)

		go func() {
			defer wg.Done()
			collect(ctx, ch)
		}()
	}

	wg.Wait()
}

// collectCrawlRuns는 crawl_runs에서 수집·스킵 누적 건수를 읽는다.
func (c *NewsLabCollector) collectCrawlRuns(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select
			coalesce(sum(inserted_count), 0),
			coalesce(sum(skipped_count), 0)
		from crawl_runs
	`

	var collected float64
	var skipped float64

	row := c.db.QueryRowContext(ctx, query)
	err := row.Scan(&collected, &skipped)
	if err != nil {
		c.reportQueryFailure(ch, "crawl_runs", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.articlesCollected, prometheus.CounterValue, collected,
	)
	ch <- prometheus.MustNewConstMetric(
		c.articlesSkipped, prometheus.CounterValue, skipped,
	)
	ch <- prometheus.MustNewConstMetric(
		c.querySuccess, prometheus.GaugeValue, 1, "crawl_runs",
	)
}

// collectPipelineRuns는 파이프라인별·상태별 실행 횟수를 읽는다.
func (c *NewsLabCollector) collectPipelineRuns(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select 'rss_collector' as pipeline, status, count(*) from crawl_runs group by status
		union all
		select 'extraction', status, count(*) from extraction_runs group by status
		union all
		select 'three_day', status, count(*) from three_day_topic_runs group by status
		union all
		select 'weekly', status, count(*) from weekly_topic_runs group by status
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_runs", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var pipeline string
		var status string
		var runs float64

		err := rows.Scan(&pipeline, &status, &runs)
		if err != nil {
			c.reportQueryFailure(ch, "pipeline_runs", err)
			return
		}

		ch <- prometheus.MustNewConstMetric(
			c.pipelineRuns, prometheus.CounterValue, runs, pipeline, status,
		)
	}

	err = rows.Err()
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_runs", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(
		c.querySuccess, prometheus.GaugeValue, 1, "pipeline_runs",
	)
}

// collectStoredCounts는 현재 저장된 기사와 임베딩 수를 읽는다.
func (c *NewsLabCollector) collectStoredCounts(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select
			(select count(*) from articles),
			(select count(*) from article_embeddings)
	`

	var articles float64
	var embeddings float64

	row := c.db.QueryRowContext(ctx, query)
	err := row.Scan(&articles, &embeddings)
	if err != nil {
		c.reportQueryFailure(ch, "stored_counts", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.articlesStored, prometheus.GaugeValue, articles)
	ch <- prometheus.MustNewConstMetric(c.embeddingsStored, prometheus.GaugeValue, embeddings)
	ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, "stored_counts")
}

// collectPipelineLastSuccess는 파이프라인별 마지막 성공 시각을 읽는다.
// 한 번도 성공한 적이 없으면 지표를 내보내지 않는다. 0을 내보내면 1970년으로 표시되기 때문이다.
func (c *NewsLabCollector) collectPipelineLastSuccess(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select 'rss_collector' as pipeline, extract(epoch from max(finished_at))
		from crawl_runs where status = 'success'
		union all
		select 'extraction', extract(epoch from max(finished_at))
		from extraction_runs where status = 'success'
		union all
		select 'three_day', extract(epoch from max(finished_at))
		from three_day_topic_runs where status = 'success'
		union all
		select 'weekly', extract(epoch from max(finished_at))
		from weekly_topic_runs where status = 'success'
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		c.reportQueryFailure(ch, "last_success", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var pipeline string
		var epoch sql.NullFloat64

		err := rows.Scan(&pipeline, &epoch)
		if err != nil {
			c.reportQueryFailure(ch, "last_success", err)
			return
		}

		if !epoch.Valid {
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			c.pipelineLastSuccess, prometheus.GaugeValue, epoch.Float64, pipeline,
		)
	}

	err = rows.Err()
	if err != nil {
		c.reportQueryFailure(ch, "last_success", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, "last_success")
}

// collectPipelineEmbeddings는 파이프라인별 임베딩 후보·재사용·미보유 건수를 읽는다.
func (c *NewsLabCollector) collectPipelineEmbeddings(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select 'three_day' as pipeline,
			coalesce(sum(candidate_count), 0),
			coalesce(sum(embedding_count), 0),
			coalesce(sum(missing_embedding_count), 0)
		from three_day_topic_runs
		union all
		select 'weekly',
			coalesce(sum(candidate_count), 0),
			coalesce(sum(embedding_count), 0),
			coalesce(sum(missing_embedding_count), 0)
		from weekly_topic_runs
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_embeddings", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var pipeline string
		var candidates float64
		var reused float64
		var missing float64

		err := rows.Scan(&pipeline, &candidates, &reused, &missing)
		if err != nil {
			c.reportQueryFailure(ch, "pipeline_embeddings", err)
			return
		}

		ch <- prometheus.MustNewConstMetric(c.embeddingCandidates, prometheus.CounterValue, candidates, pipeline)
		ch <- prometheus.MustNewConstMetric(c.embeddingReused, prometheus.CounterValue, reused, pipeline)
		ch <- prometheus.MustNewConstMetric(c.embeddingMissing, prometheus.CounterValue, missing, pipeline)
	}

	err = rows.Err()
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_embeddings", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, "pipeline_embeddings")
}

// collectPipelineTopics는 파이프라인별 토픽 선정·저장·실패 수를 읽는다.
func (c *NewsLabCollector) collectPipelineTopics(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select 'three_day' as pipeline,
			coalesce(sum(selected_topic_count), 0),
			coalesce(sum(saved_topic_count), 0),
			coalesce(sum(failed_topic_count), 0)
		from three_day_topic_runs
		union all
		select 'weekly',
			coalesce(sum(selected_topic_count), 0),
			coalesce(sum(saved_topic_count), 0),
			coalesce(sum(failed_topic_count), 0)
		from weekly_topic_runs
	`

	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_topics", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var pipeline string
		var selected float64
		var saved float64
		var failed float64

		err := rows.Scan(&pipeline, &selected, &saved, &failed)
		if err != nil {
			c.reportQueryFailure(ch, "pipeline_topics", err)
			return
		}

		ch <- prometheus.MustNewConstMetric(c.topicsSelected, prometheus.CounterValue, selected, pipeline)
		ch <- prometheus.MustNewConstMetric(c.topicsSaved, prometheus.CounterValue, saved, pipeline)
		ch <- prometheus.MustNewConstMetric(c.topicsFailed, prometheus.CounterValue, failed, pipeline)
	}

	err = rows.Err()
	if err != nil {
		c.reportQueryFailure(ch, "pipeline_topics", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, "pipeline_topics")
}

// collectExtractionItems는 원문 추출의 성공·실패 건수를 읽는다.
// extraction_runs는 항목이 실패해도 run status를 success로 기록하므로
// 실패 건수를 별도 지표로 노출해야 한다.
func (c *NewsLabCollector) collectExtractionItems(ctx context.Context, ch chan<- prometheus.Metric) {
	const query = `
		select
			coalesce(sum(success_count), 0),
			coalesce(sum(failed_count), 0)
		from extraction_runs
	`

	var processed float64
	var failed float64

	row := c.db.QueryRowContext(ctx, query)
	err := row.Scan(&processed, &failed)
	if err != nil {
		c.reportQueryFailure(ch, "extraction_items", err)
		return
	}

	ch <- prometheus.MustNewConstMetric(c.itemsProcessed, prometheus.CounterValue, processed, "extraction")
	ch <- prometheus.MustNewConstMetric(c.itemsFailed, prometheus.CounterValue, failed, "extraction")
	ch <- prometheus.MustNewConstMetric(c.querySuccess, prometheus.GaugeValue, 1, "extraction_items")
}

// reportQueryFailure는 쿼리 실패를 로그로 남기고 query_success=0을 내보낸다.
// query_success만으로는 실패 원인을 알 수 없기 때문이다.
func (c *NewsLabCollector) reportQueryFailure(ch chan<- prometheus.Metric, query string, err error) {
	log.Printf("collector query failed: query=%s err=%v", query, err)

	ch <- prometheus.MustNewConstMetric(
		c.querySuccess, prometheus.GaugeValue, 0, query,
	)
}
