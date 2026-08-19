# Verification: NewsLab pipeline metrics exporter (Go)

## Verification Status

passed

UNIT-01 ~ UNIT-07 완료. 운영 클러스터에서 Prometheus 수집을 확인했다.

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

## UNIT-05. 컨테이너화

### 구현 범위

- multi-stage `Dockerfile` (`golang:1.26-alpine` build → `distroless/static-debian12:nonroot` runtime)
- `.dockerignore`
- `main.go`에 `/healthz` 추가 (probe 용도)

### 빌드

Command:

```bash
docker build -t news-lab-exporter:local .
```

Status: passed

### 이미지 크기 측정

Command:

```bash
docker images news-lab-exporter:local --format '{{.Size}}'
docker images seocj/news-api --format '{{.Repository}} {{.Size}}'
```

Result:

```text
25MB
seocj/news-api 58.5MB
```

| 이미지 | 기반 | 크기 |
| --- | --- | --- |
| `news-lab-exporter` | Go + distroless/static | **25 MB** |
| `seocj/news-api` | Python 3.12-slim | **58.5 MB** |

**감소율 57%** (58.5 → 25 MB).

Notes:

- **사전 추정과 실측이 달랐다.** `학습_Go-왜-쓰는가.md`에는 Python 150~400MB,
  Go distroless 10~20MB로 적어뒀으나 실측은 각각 58.5MB, 25MB였다.
  - 기존 이미지가 `python:3.12-slim` 기반이라 이미 가볍다
  - Go 바이너리에 pgx와 prometheus 클라이언트가 포함되어 20MB를 넘었다
- 일반적으로 인용되는 "10배 차이"는 이 프로젝트에 해당하지 않는다.
  **추정치가 아니라 실측치를 근거로 쓴다.**
- 이 규모에서는 이미지 크기보다 **런타임 설치가 필요 없다는 점**이 실질적 이점이다.

### 컨테이너 동작 확인

Command:

```bash
set -a; source .env; set +a
docker run --rm -p 9310:9310 -e DATABASE_URL="$DATABASE_URL" news-lab-exporter:local

curl -s localhost:9310/healthz
curl -s localhost:9310/metrics | grep query_success
```

Result:

```text
2026/08/19 13:42:21 db connected (read only)
2026/08/19 13:42:21 listening on :9310

ok

newslab_exporter_query_success{query="crawl_runs"} 1
newslab_exporter_query_success{query="extraction_items"} 1
newslab_exporter_query_success{query="last_success"} 1
newslab_exporter_query_success{query="pipeline_embeddings"} 1
newslab_exporter_query_success{query="pipeline_runs"} 1
newslab_exporter_query_success{query="pipeline_topics"} 1
newslab_exporter_query_success{query="stored_counts"} 1
```

Status: passed

Notes:

- distroless 이미지에는 셸이 없으므로 exec probe를 쓸 수 없다. httpGet probe만 가능하다.
- `/healthz`는 DB를 조회하지 않는다. 아래 UNIT-06 참조.

---

## UNIT-06. Kubernetes manifest

### 구현 범위

- `k8s/deployment.yaml` — replica 1, `nodeSelector: workload=app`, securityContext, probe
- `k8s/service.yaml` — ClusterIP, named port `metrics`
- `k8s/servicemonitor.yaml` — `release: monitoring` label

### 설계 판단

**replica는 1로 고정한다.**
지표 값이 DB에서 오므로 replica를 늘려도 값이 같다. Prometheus가 instance별로 중복
수집해 운영 DB 조회만 두 배가 된다.

**probe는 `/metrics`를 때리지 않는다.**
`/metrics` 한 번이 DB 조회 7개를 유발한다. probe를 거기 걸면 감시자가 감시 대상에
주기적으로 부하를 준다.

**probe를 DB 상태에 연동하지 않는다.**
DB 장애 시 Pod가 `NotReady`가 되면 Prometheus가 scrape를 멈춘다.
그 순간이 바로 `query_success=0`을 확인해야 할 때다.
**감시자가 감시 대상과 함께 죽으면 안 된다.**

**타임아웃 순서**: exporter 내부 `context` 10s < `scrapeTimeout` 30s < `interval` 60s.
안쪽이 먼저 끊겨야 어느 쿼리가 느린지 `query_success`로 드러난다.

`interval`이 60s인 이유는 이 지표들이 하루 단위 CronJob 결과이기 때문이다.
더 자주 긁어도 값이 바뀌지 않고 운영 DB 조회만 늘어난다.

### ServiceMonitor label

`news-lab/k8s/monitoring/kube-prometheus-stack-values.yaml`에
`serviceMonitorSelectorNilUsesHelmValues` 설정이 없어 chart 기본값 `true`가 적용된다.
이 경우 Prometheus는 **Helm release 이름 label이 붙은 ServiceMonitor만 수집**한다.

```yaml
metadata:
  labels:
    release: monitoring
```

이 label이 없으면 `apply`가 성공해도 Prometheus가 조용히 무시한다. 오류가 나지 않는다.

### Secret

기존 `news-api-secret`의 `DATABASE_URL`은 SQLAlchemy 형식(`postgresql+psycopg://`)이라
pgx가 파싱하지 못한다. **exporter 전용 Secret이 필요하다.**

```bash
kubectl create secret generic news-lab-exporter-secret \
  --from-literal=DATABASE_URL='postgresql://<read-only 계정>:<비밀번호>@<host>:6543/postgres'
```

read-only 계정 발급과 Secret 생성은 UNIT-07의 사람 수행 항목이다.

### manifest 검증

1차 시도는 `KUBECONFIG` 미설정으로 실패했다.

```text
error validating "k8s/deployment.yaml": failed to download openapi:
Get "http://localhost:8080/openapi/v2?timeout=32s": dial tcp [::1]:8080: connect: connection refused
```

manifest 문제가 아니라 kubectl이 클러스터를 찾지 못한 것이었다.

Command:

```bash
KUBECONFIG=~/.kube/oci-k3s.yaml kubectl apply --dry-run=client -f k8s/
```

Result:

```text
deployment.apps/news-lab-exporter created (dry run)
service/news-lab-exporter created (dry run)
servicemonitor.monitoring.coreos.com/news-lab-exporter created (dry run)
```

Status: passed

Notes:

- `ServiceMonitor`가 인식됐다는 것은 클러스터에 Prometheus Operator CRD가 존재하고
  manifest가 해당 스키마에 유효하다는 뜻이다. 실제 적용(`--dry-run` 없이)은 사람이 수행한다.
- **DB나 Prometheus에 주소를 적은 곳이 없다.** Deployment가 Pod에 `app=news-lab-exporter`
  label을 붙이고, Service가 그 label로 Pod를 찾고, ServiceMonitor가 그 label로 Service를
  찾는다. Prometheus Operator가 이를 읽어 scrape 설정을 자동 생성한다.
  Pod IP가 바뀌어도 설정을 고칠 필요가 없다.
- Prometheus는 Service를 경유하지 않고 Pod IP로 직접 scrape한다.
  Service는 대상 발견(Endpoints) 용도다.

### 미수행

- `.github/workflows/docker-build.yml` 작성 — 원격 도구로 쓸 수 없는 보호 경로라 수동 생성 필요

---

## UNIT-07. 운영 반영 (사람 수행)

수행일: 2026-08-19 ~ 08-20

### ① read-only 역할 발급

Command (Supabase SQL Editor):

```sql
create role newslab_exporter with login password '<생성한 32자 영숫자>';
grant connect on database postgres to newslab_exporter;
grant usage on schema public to newslab_exporter;
grant select on
  crawl_runs, extraction_runs, three_day_topic_runs,
  weekly_topic_runs, articles, article_embeddings
to newslab_exporter;
```

Status: passed

Notes:

- exporter가 읽는 6개 table에만 `select`를 부여했다. `all tables`가 편하지만
  지표를 추가할 때 권한을 다시 주게 하는 편이 "이 계정이 무엇을 읽는지"를 명시적으로 유지한다.
- **Supavisor pooler(6543)는 사용자명이 `<역할명>.<프로젝트ref>` 형식이어야 한다.**
  어느 프로젝트로 라우팅할지 판단하는 데 쓰이며, 이를 빠뜨리면 인증에 실패한다.
- 비밀번호는 영숫자만으로 생성했다. `base64`가 만드는 `+`, `/`는 접속 문자열에서
  URL 인코딩이 필요해 파싱 문제를 일으킨다.

### ② read-only 동작 검증 ★ UNIT-02부터 미검증이던 항목

`db.Ping()` 직후에 임시 코드를 넣어 쓰기를 시도했다.

```go
_, writeErr := db.Exec("create table zzz_readonly_check(i int)")
log.Printf(">>> write attempt: %v", writeErr)
```

Result:

```text
>>> write attempt: ERROR: permission denied for schema public (SQLSTATE 42501)
```

Status: passed

Notes:

- **권한 레벨이 먼저 막았다.** 계정에 `create` 권한이 없어 트랜잭션 모드 검사까지 가지 않았다.
- 따라서 **코드 레벨(`default_transaction_read_only`)은 여전히 단독으로 검증되지 않았다.**
  분리 검증하려면 기존 계정으로 같은 코드를 실행해
  `cannot execute CREATE TABLE in a read-only transaction (SQLSTATE 25006)`을 확인해야 한다.
- 검증 후 임시 코드는 삭제했다.

### ③ Secret 생성

Command:

```bash
set -a; source .env; set +a
echo "${#DATABASE_URL}"        # 117

kubectl create secret generic news-lab-exporter-secret \
  --from-literal=DATABASE_URL="$DATABASE_URL"

kubectl describe secret news-lab-exporter-secret
```

Result:

```text
DATABASE_URL:  117 bytes
```

Status: passed

Notes:

- 첫 시도에서 `DATABASE_URL: 0 bytes`로 생성됐다. `source .env`를 실행한 셸과
  `kubectl create secret`을 실행한 셸이 달라 변수가 비어 있었다.
  **`echo "${#DATABASE_URL}"`로 길이를 먼저 확인하는 절차를 넣었다.**
- Secret 값은 git에 없다. 클러스터를 재구축하면 이 절차를 사람이 다시 수행해야 한다.

### ④ 배포 중 만난 문제 두 가지

**1. 자리표시자 태그로 image pull 실패**

```text
Failed to pull image "seocj/news-lab-exporter:REPLACE_WITH_GIT_SHA": not found
```

원인: `feature` 브랜치에서 `kubectl apply`를 실행했다. 이미지 태그를 실제 SHA로 바꾼
`update-manifest` PR은 `main`에 머지됐으므로 feature 브랜치에는 반영되지 않았다.

**배포한 것과 커밋된 것이 달랐다.** GitOps가 방지하려는 상황이며, 자리표시자였기에
즉시 실패해 드러났다. `latest` 같은 값이었다면 조용히 다른 이미지가 떴을 것이다.

조치: `main`으로 전환·pull 후 재적용. **이후 apply는 항상 `main`에서 수행한다.**

**2. distroless + `runAsNonRoot` 충돌**

```text
container has runAsNonRoot and image has non-numeric user (nonroot),
cannot verify user is non-root
```

원인: distroless `:nonroot` 이미지는 `USER`를 이름으로 지정한다.
`runAsNonRoot: true`는 숫자 UID여야 root 여부를 검증할 수 있다.

조치: `runAsUser: 65532`, `runAsGroup: 65532`를 명시했다.

### ⑤ 배포 확인

Command:

```bash
kubectl rollout status deployment/news-lab-exporter
kubectl get pods -l app=news-lab-exporter
kubectl logs -l app=news-lab-exporter --tail=20
```

Result:

```text
deployment "news-lab-exporter" successfully rolled out

NAME                                 READY   STATUS    RESTARTS   AGE
news-lab-exporter-7c96f495f7-5wrsz   1/1     Running   0          2m17s

2026/08/19 15:36:16 db connected (read only)
2026/08/19 15:36:16 listening on :9310
```

이미지 pull 크기: **5,598,979 bytes** (kubelet 보고값). Docker Hub 레지스트리 값과 일치한다.

Status: passed

### ⑥ 클러스터 내 지표 확인

Command:

```bash
kubectl port-forward deploy/news-lab-exporter 9310:9310
curl -s localhost:9310/metrics | grep query_success
```

Result:

```text
newslab_exporter_query_success{query="crawl_runs"} 1
newslab_exporter_query_success{query="extraction_items"} 1
newslab_exporter_query_success{query="last_success"} 1
newslab_exporter_query_success{query="pipeline_embeddings"} 1
newslab_exporter_query_success{query="pipeline_runs"} 1
newslab_exporter_query_success{query="pipeline_topics"} 1
newslab_exporter_query_success{query="stored_counts"} 1
```

Status: passed — read-only 계정으로 6개 table 모두 조회 가능함을 확인했다.

### ⑦ Prometheus 수집 확인 ★ 최종 검증

Command:

```bash
kubectl get servicemonitor -n default -o custom-columns=NAME:.metadata.name,RELEASE:.metadata.labels.release
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
curl -s 'localhost:9090/api/v1/query?query=newslab_pipeline_runs_total'
```

Result:

```text
NAME                RELEASE
news-lab-exporter   monitoring

{"status":"success","data":{"resultType":"vector","result":[
  {"metric":{"__name__":"newslab_pipeline_runs_total",
             "container":"news-lab-exporter","endpoint":"metrics",
             "instance":"10.42.1.71:9310","job":"news-lab-exporter",
             "namespace":"default","pipeline":"extraction",
             "pod":"news-lab-exporter-7c96f495f7-5wrsz",
             "service":"news-lab-exporter","status":"success"},
   "value":[1787154122.807,"295"]}, ...
```

Status: passed

Notes:

- **Prometheus가 exporter를 발견하고 60초 간격으로 수집하고 있다.**
- 우리가 정의한 label은 `pipeline`, `status` 둘뿐이다.
  `job`, `instance`, `namespace`, `pod`, `service`, `container`, `endpoint`는
  ServiceMonitor 기반 서비스 디스커버리가 자동으로 부여한 것이다.
  **manifest 어디에도 IP나 주소를 기록하지 않았다.**
- 기존 ServiceMonitor 13개가 모두 `release: monitoring`을 사용하며 우리 것과 일치한다.

### 미수행

- Grafana 패널 추가 (`news-lab` 저장소의 별도 Task)
- 신선도·부분 실패 알림 규칙 (`news-lab` 저장소)
- 코드 레벨 read-only 단독 검증 (②번 Notes 참조)

## 남은 작업

이 Task 범위 밖이거나 다른 저장소의 작업이다.

- Grafana 패널 추가 (`news-lab` 저장소)
- 신선도·부분 실패 알림 규칙 (`news-lab` 저장소)
- 코드 레벨 read-only 단독 검증 — 권한 레벨이 먼저 막아 분리 검증되지 않았다
- `Collect()` 통합 테스트와 나머지 collect 함수 테스트

## 수행한 사람 작업 (재구축 시 반복 필요)

- Supabase read-only 역할 발급, `grant select` 6개 table
- `news-lab-exporter-secret` 생성 (값은 git에 없다)
- `kubectl apply -f k8s/` — 반드시 `main` 브랜치에서
- Prometheus Targets `UP` 확인


- Supabase에서 read-only 역할 발급 및 Secret 생성
- 운영 K3s 적용, 이미지 push
- Prometheus target `UP` 확인
- Grafana 패널 추가 (`news-lab` 저장소의 별도 Task)
