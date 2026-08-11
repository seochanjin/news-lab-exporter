# Verification: NewsLab pipeline metrics exporter (Go)

## Verification Status

pending

UNIT-01 ~ UNIT-04 완료. UNIT-05~07 미수행.

## 환경

| 항목 | 값 |
| --- | --- |
| Go | `go1.26.5 darwin/arm64` |
| 실행 환경 | 로컬 macOS |
| DB | 운영 Supabase (Supavisor transaction pooler, port 6543) |
| DB 계정 | **기존 파이프라인용 계정.** read-only 전용 계정은 미발급 (UNIT-07 사람 수행 항목) |

---

## UNIT-01. Go 프로젝트 뼈대와 `/metrics` 하드코딩 노출

Command:

```bash
go mod init github.com/seochanjin/news-lab-exporter
go mod tidy
go run .
curl -s localhost:9310/metrics | grep newslab
```

Result:

```text
# HELP newslab_exporter_build_info Exporter build info. Always 1.
# TYPE newslab_exporter_build_info gauge
newslab_exporter_build_info 1
```

Status: passed

Notes:

- `LISTEN_ADDR` 기본값을 `:9100`에서 `:9310`으로 변경했다.
  `node_exporter`가 9100을 사용하며 운영 클러스터에서 이미 실행 중이다.
- DB 연결 없음. `promauto.NewGauge`로 하드코딩 gauge 1종만 노출했다.
- `client_golang`이 기본 registry에 Go 런타임(`go_*`)과 프로세스(`process_*`) 지표를
  자동 등록하므로 `/metrics` 전체 출력에는 이들이 함께 나온다. 의도된 동작이다.

---

## UNIT-02. 설정과 DB 연결 (read-only)

### 구현 범위

- `internal/config/config.go` — 환경변수 기반 `Config` 구조체와 `Load()`
- `main.go` — `openReadOnlyDB()`로 read-only 연결 생성, `db.Ping()`으로 확인

### 필수값 누락 시 기동 실패 확인

Command:

```bash
go run .        # DATABASE_URL 없이
```

Result:

```text
config load failed: DATABASE_URL is required
exit status 1
```

Status: passed

Notes:

- 필수 설정이 없으면 반쯤 동작하는 상태로 뜨지 않고 즉시 종료하도록 했다.

### DB 연결 확인

Command:

```bash
set -a
source .env
set +a
go run .
```

Result:

```text
2026/08/10 11:25:17 db connected (read only)
2026/08/10 11:25:17 listening on :9310
```

Status: passed

Notes:

- read-only 강제는 쿼리마다 `set transaction read only`를 실행하는 대신
  **pgx `RuntimeParams`에 `default_transaction_read_only = on`을 설정**하는 방식으로 구현했다.
  커넥션 풀이 새 연결을 만들어도 자동 적용되므로 빠뜨릴 수 없다.
- Supavisor transaction pooler(port 6543)가 이 startup parameter를 거부하지 않았다.
  연결이 성공했으므로 파라미터가 수용된 것으로 판단한다.
- **read-only 동작 자체는 아직 직접 검증하지 않았다.** 쓰기 시도로 거부되는지는
  UNIT-03에서 실제 쿼리를 붙일 때 확인한다. 현재는 "연결 성공"까지만 확인했다.

### 조사 중 발견한 문제

**1. `go.mod`의 `// indirect` 표시로 빌드 실패**

Result:

```text
missing go.sum entry for module providing package github.com/jackc/puddle/v2
```

원인: `go get github.com/jackc/pgx/v5`를 import 작성 **전에** 실행해서
Go가 pgx를 간접 의존으로 기록했다. 이후 import를 추가했으나 `go.mod`를 갱신하지 않았다.

조치: `go mod tidy` 실행. pgx가 직접 의존으로 승격되고 누락된 `go.sum` 항목이 채워졌다.

**2. `DATABASE_URL` 형식 불일치**

Result:

```text
cannot parse `postgresql+psycopg://...`: failed to parse as keyword/value (invalid keyword/value)
```

원인: `news-lab`의 `.env`는 SQLAlchemy 형식(`dialect+driver://`)을 사용한다.
`postgresql+psycopg://`는 SQLAlchemy만 해석하며 pgx를 포함한 표준 클라이언트는 인식하지 못한다.

조치: exporter 저장소의 `.env`에는 `postgresql://` 표준 형식으로 별도 기록했다.
같은 DB를 언어별로 다른 형식으로 참조하는 것이 정상이므로 두 저장소의 값을 통일하지 않는다.

---

## UNIT-03. Collector 구현과 쿼리 7개

### 구현 범위

- `internal/collector/collector.go` — `prometheus.Collector` 인터페이스 직접 구현
- `Describe()` / `Collect()` 두 메서드로 인터페이스 충족 (`implements` 선언 없음)
- 지표 15종, 쿼리 7개
- `main.go`에서 `prometheus.MustRegister(newslabCollector)`로 등록

### 지표 노출 확인

Command:

```bash
go mod tidy
set -a; source .env; set +a
go run .
curl -s localhost:9310/metrics | grep '^newslab' | sort
```

Result:

```text
newslab_article_embeddings_stored 7176
newslab_articles_collected_total 9750
newslab_articles_skipped_total 3966
newslab_articles_stored 9766
newslab_exporter_build_info 1
newslab_exporter_query_success{query="crawl_runs"} 1
newslab_exporter_query_success{query="extraction_items"} 1
newslab_exporter_query_success{query="last_success"} 1
newslab_exporter_query_success{query="pipeline_embeddings"} 1
newslab_exporter_query_success{query="pipeline_runs"} 1
newslab_exporter_query_success{query="pipeline_topics"} 1
newslab_exporter_query_success{query="stored_counts"} 1
newslab_pipeline_embedding_candidates_total{pipeline="three_day"} 21075
newslab_pipeline_embedding_candidates_total{pipeline="weekly"} 9752
newslab_pipeline_embedding_missing_total{pipeline="three_day"} 612
newslab_pipeline_embedding_missing_total{pipeline="weekly"} 1794
newslab_pipeline_embedding_reused_total{pipeline="three_day"} 20463
newslab_pipeline_embedding_reused_total{pipeline="weekly"} 7958
newslab_pipeline_items_failed_total{pipeline="extraction"} 50
newslab_pipeline_items_processed_total{pipeline="extraction"} 671
newslab_pipeline_last_success_timestamp_seconds{pipeline="extraction"} 1.786345632214171e+09
newslab_pipeline_last_success_timestamp_seconds{pipeline="rss_collector"} 1.786344836385017e+09
newslab_pipeline_last_success_timestamp_seconds{pipeline="three_day"} 1.786046774298005e+09
newslab_pipeline_last_success_timestamp_seconds{pipeline="weekly"} 1.785685096817905e+09
newslab_pipeline_runs_total{pipeline="extraction",status="success"} 260
newslab_pipeline_runs_total{pipeline="rss_collector",status="success"} 74
newslab_pipeline_runs_total{pipeline="three_day",status="partial_success"} 18
newslab_pipeline_runs_total{pipeline="three_day",status="success"} 32
newslab_pipeline_runs_total{pipeline="weekly",status="partial_success"} 2
newslab_pipeline_runs_total{pipeline="weekly",status="success"} 8
newslab_pipeline_topics_failed_total{pipeline="three_day"} 21
newslab_pipeline_topics_failed_total{pipeline="weekly"} 3
newslab_pipeline_topics_saved_total{pipeline="three_day"} 229
newslab_pipeline_topics_saved_total{pipeline="weekly"} 41
newslab_pipeline_topics_selected_total{pipeline="three_day"} 250
newslab_pipeline_topics_selected_total{pipeline="weekly"} 44
```

Status: passed

### 계약 대조

| 항목 | 결과 |
| --- | --- |
| 지표 15종 전부 노출 | passed |
| `pipeline` label에 `daily` 없음 | passed |
| `status` label 하드코딩 없음 (DB `group by` 결과 사용) | passed |
| 비율 계산 없이 원자값만 노출 | passed |
| 고유값(`article_id` 등) label 없음 | passed |
| `query_success` 7개 전부 `1` | passed |
| 값이 `docs/design/metrics.md` 조사값과 정합 | passed (아래) |

### 조사값과의 정합성

8/9 `psql` 조사값 대비 8/10 실측값이 모두 증가 방향으로만 변화했다.
감소한 지표는 없다. Counter 단조증가 전제가 유지됨을 확인했다.

| 지표 | 8/9 조사 | 8/10 실측 |
| --- | --- | --- |
| `articles_collected_total` | 9,613 | 9,750 |
| `articles_stored` | 9,629 | 9,766 |
| `article_embeddings_stored` | 7,061 | 7,176 |
| `pipeline_runs{three_day,partial_success}` | 17 | 18 |
| `pipeline_runs{three_day,success}` | 32 | 32 |
| `items_failed{extraction}` | 47 | 50 |

### 구현 중 수정한 결함

**`rows.Err()` 미확인**

최초 구현에서 `for rows.Next()` 반복 후 `rows.Err()`를 확인하지 않았다.
반복 중간에 조회가 끊기면 `rows.Next()`가 `false`를 반환하며 루프가 정상 종료된 것처럼
빠져나오고, 부분 데이터를 받은 상태로 `query_success = 1`을 내보내게 된다.

**이는 이 exporter가 드러내려는 `extraction_runs`의 문제와 동일한 패턴이다.**
항목이 실패해도 전체를 성공으로 기록하는 것.

조치: 모든 다중 행 조회 함수에서 반복 후 `rows.Err()`를 확인하고,
오류가 있으면 `query_success = 0`을 내보내도록 수정했다.

### 계약 변경

`newslab_exporter_scrape_error_total` (Counter) → `newslab_exporter_query_success` (Gauge).
근거는 `docs/design/metrics.md`의 "구현 중 변경한 계약" 절에 기록했다.

### 이 시점의 미확인 항목

- **read-only 동작의 실제 거부는 미검증.** 이번 UNIT의 쿼리가 모두 `select`라
  쓰기 거부를 확인할 기회가 없었다.
- 쿼리 7개는 순차 실행된다. 동시 실행은 UNIT-03c에서 적용했다.
- `gofmt` / `go vet` / `go test` 미실행. UNIT-04에서 수행했다.

---

## UNIT-03c. 쿼리 동시 실행

### 구현 범위

- `Collect()`에서 쿼리 7개를 `sync.WaitGroup`으로 동시 실행
- `context.WithTimeout(cfg.ScrapeTimeout)` 적용, 전 조회를 `QueryContext`/`QueryRowContext`로 전환
- `reportQueryFailure` 헬퍼로 실패 시 로그 기록

### 계약 변경 — `errgroup` → `sync.WaitGroup`

Task 문서는 `errgroup`을 명시했으나 구현 단계에서 `sync.WaitGroup`으로 변경했다.

| `errgroup` 기능 | 이 프로젝트에 필요한가 |
| --- | --- |
| 에러 전파 | 불필요. 각 함수가 `query_success=0`으로 자체 처리 |
| `WithContext` (하나 실패 시 나머지 취소) | **요구와 반대.** 하나가 실패해도 나머지 지표는 나와야 한다 |
| `SetLimit` | 불필요 (7개 고정) |

에러를 전파하지 않는데 `errgroup`을 쓰면 전 함수가 `return nil`만 하게 된다.
필요해서가 아니라 관례로 쓰는 것이 되므로 `sync.WaitGroup`을 택했다.

### 동시 실행 도입 직후 발생한 장애 ★

Command:

```bash
go run .
curl -s localhost:9310/metrics | grep query_success
```

Result (1차):

```text
newslab_exporter_query_success{query="crawl_runs"} 0
newslab_exporter_query_success{query="extraction_items"} 0
newslab_exporter_query_success{query="last_success"} 0
newslab_exporter_query_success{query="pipeline_embeddings"} 1
newslab_exporter_query_success{query="pipeline_runs"} 0
newslab_exporter_query_success{query="pipeline_topics"} 0
newslab_exporter_query_success{query="stored_counts"} 0
```

Status: failed

Notes:

- 순차 실행에서는 7개 모두 성공했으나 동시 실행으로 바꾸자 5~6개가 실패했다.
- **매 scrape마다 성공하는 쿼리가 달랐다.** 특정 쿼리의 문제가 아니라 공유 자원 경쟁임을 시사했다.
- **실패한 쿼리의 업무 지표는 하나도 노출되지 않았고, 성공한 쿼리의 지표만 노출됐다.**
  "하나가 실패해도 나머지는 내보낸다"는 설계가 실제 장애에서 의도대로 동작했다.
  `errgroup.WithContext`를 쓰지 않은 판단이 여기서 검증됐다.

### 원인 진단 — 로그 추가 후 확정

당시 코드는 `err`를 받아 버리고 `query_success=0`만 내보냈다.
**실패 사실은 알 수 있으나 원인을 알 수 없어 진단이 불가능했다.**

조치: `reportQueryFailure` 헬퍼를 추가해 실패를 로그에 남기도록 수정했다.

Result:

```text
collector query failed: query=crawl_runs err=ERROR: prepared statement
"stmtcache_19b81018f4887842a4b5693cc475cae7f1a238e22b7384f7" does not exist (SQLSTATE 26000)
```

원인: Supabase transaction pooler(port 6543)는 트랜잭션마다 backend connection을 재배정한다.
pgx 기본값인 extended protocol은 PREPARE한 backend와 EXECUTE하는 backend가 달라지면 실패한다.
순차 실행에서는 커넥션 하나를 재사용해 드러나지 않았고, 동시 실행이 방아쇠가 됐다.

### 조치

```go
connCfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
db.SetMaxOpenConns(maxConns)
db.SetMaxIdleConns(maxConns)
```

- simple protocol은 prepared statement를 만들지 않으므로 backend가 바뀌어도 무관하다
- `DB_MAX_CONNS`(기본 4) 설정을 추가했다. 감시자가 감시 대상의 커넥션을 무제한 점유하면 안 된다.
  Task 문서 "설정" 절에 있었으나 구현이 누락됐던 항목이다

Result (조치 후):

```text
newslab_exporter_query_success{query="crawl_runs"} 1
newslab_exporter_query_success{query="extraction_items"} 1
newslab_exporter_query_success{query="last_success"} 1
newslab_exporter_query_success{query="pipeline_embeddings"} 1
newslab_exporter_query_success{query="pipeline_runs"} 1
newslab_exporter_query_success{query="pipeline_topics"} 1
newslab_exporter_query_success{query="stored_counts"} 1
```

Status: passed

지표 35줄이 순차 실행 시점과 동일함을 확인했다.

---

## UNIT-04. 테스트

### 구현 범위

- `internal/config/config_test.go` — 순수 로직 테스트 (mock 불필요)
- `internal/collector/collector_test.go` — `go-sqlmock`으로 DB를 대체

Command:

```bash
go test ./... -v
```

Result:

```text
=== RUN   TestCollectCrawlRuns_정상이면_지표를_내보낸다
--- PASS
=== RUN   TestCollectCrawlRuns_쿼리가_실패하면_업무지표를_내보내지_않는다
2026/08/11 23:25:42 collector query failed: query=crawl_runs err=연결 끊김
--- PASS
=== RUN   TestCollectPipelineLastSuccess_성공이력이_없으면_지표를_생략한다
--- PASS
ok      github.com/seochanjin/news-lab-exporter/internal/collector    0.557s

=== RUN   TestGetEnvInt
    --- PASS: 값이_없으면_기본값을_쓴다
    --- PASS: 정수_문자열을_파싱한다
    --- PASS: 정수가_아니면_오류
    --- PASS: 0이면_오류
    --- PASS: 음수면_오류
=== RUN   TestLoad_DATABASE_URL이_없으면_실패한다
--- PASS
ok      github.com/seochanjin/news-lab-exporter/internal/config
```

Status: passed (9 케이스)

Notes:

- 실패 테스트 실행 중 `collector query failed: ... err=연결 끊김` 로그가 출력됐다.
  실패 경로가 실제로 실행됐다는 증거다.
- 테스트는 실제 DB와 네트워크에 접속하지 않는다. `sqlmock`이 `*sql.DB`를 대체한다.
- 실제 DB로는 "연결이 끊긴 상황"을 만들 수 없으므로 mock이 필요했다.

### 고정한 동작

| 테스트 | 고정한 원칙 |
| --- | --- |
| `쿼리가_실패하면_업무지표를_내보내지_않는다` | 실패를 `0`으로 덮지 않는다. 2026-08-10 트랜잭션 풀러 장애에서 실제 확인한 동작을 회귀 방지로 고정 |
| `성공이력이_없으면_지표를_생략한다` | `NULL`과 `0`을 구분한다. `0`은 1970-01-01로 표시되어 "한 번도 성공 안 함"과 "오래됨"을 구분할 수 없다 |
| `getEnvInt` 5케이스 | 잘못된 설정으로 반쯤 동작하는 상태를 만들지 않는다 |

### 미수행

- `Collect()` 전체를 대상으로 한 통합 테스트 (쿼리 7개 전부 mock 필요)
- 나머지 5개 collect 함수의 개별 테스트 — `collectCrawlRuns`와 구조가 동일하므로
  대표 케이스만 검증했다. 커버리지를 늘리려면 추가 필요

---

## 미수행

- UNIT-05 컨테이너화, 이미지 크기 측정
- UNIT-06 Kubernetes manifest
- UNIT-07 운영 반영 (사람 수행)
- read-only 권한 계정 발급 (사람 수행)
- **read-only 동작의 실제 거부 확인** — 모든 쿼리가 `select`라 UNIT-03·04에서도
  확인 기회가 없었다. UNIT-07 운영 반영 시 사람이 확인한다
- `Collect()` 통합 테스트와 나머지 collect 함수 테스트

## 사람이 수행할 항목

- Supabase에서 read-only 역할 발급 및 Secret 생성
- 운영 K3s 적용, 이미지 push
- Prometheus target `UP` 확인
- Grafana 패널 추가 (`news-lab` 저장소의 별도 Task)
