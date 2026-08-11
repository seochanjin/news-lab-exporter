# Task: NewsLab pipeline metrics exporter (Go) 구현

Branch: `feature/pipeline-metrics-exporter`

## Goal

NewsLab의 PostgreSQL/Supabase 실행 이력 table을 읽어 Prometheus 지표로 노출하는
Go exporter를 구현하고 운영 K3s에 배포한다.

노출할 지표와 근거는 `docs/design/metrics.md`에서 이미 확정했다. **이 Task는 그 확정된
계약을 코드로 옮기는 작업이며, 지표를 새로 발굴하거나 정의를 바꾸지 않는다.**

배경: `news-lab/docs/design/pipeline-operations-dashboard.md`가 UNIT-06에서
"Kubernetes metric으로 확인할 수 없는 업무 metric" 8개를 후속 exporter 후보로 확정하면서,
metric 이름·label cardinality·수집 경로·retention은 별도 Task에서 설계하라고 남겼다.
이 Task가 그 후속이다.

조사 단계에서 확인된 사실:

- three_day 실행의 **34.7%가 `partial_success`**인데 Kubernetes Job은 전부 성공으로 보인다
- extraction_runs는 **252회 전부 `success`인데 실패가 47건** 있다 (상태 label만으로는 안 보임)

## 전제 조건

- `docs/design/metrics.md`의 "확정된 지표 정의" 표가 이 Task의 계약이다
- 구현자는 **Go가 처음이다.** UNIT 단위를 작게 유지하고, 각 UNIT에서 새로 쓴 Go 개념을
  `docs/devlog/`에 한 줄씩이라도 기록한다

---

## Scope

### 1. 노출할 지표 (15종 / 쿼리 6개)

| 지표 | 타입 | label | 소스 |
| --- | --- | --- | --- |
| `newslab_articles_collected_total` | Counter | — | `sum(crawl_runs.inserted_count)` |
| `newslab_articles_skipped_total` | Counter | — | `sum(crawl_runs.skipped_count)` |
| `newslab_articles_stored` | Gauge | — | `count(articles)` |
| `newslab_article_embeddings_stored` | Gauge | — | `count(article_embeddings)` |
| `newslab_pipeline_embedding_candidates_total` | Counter | `pipeline` | `sum(candidate_count)` |
| `newslab_pipeline_embedding_reused_total` | Counter | `pipeline` | `sum(embedding_count)` |
| `newslab_pipeline_embedding_missing_total` | Counter | `pipeline` | `sum(missing_embedding_count)` |
| `newslab_pipeline_runs_total` | Counter | `pipeline`, `status` | `count(*) group by status` |
| `newslab_pipeline_last_success_timestamp_seconds` | Gauge | `pipeline` | `max(finished_at) where status='success'` |
| `newslab_pipeline_topics_selected_total` | Counter | `pipeline` | `sum(selected_topic_count)` |
| `newslab_pipeline_topics_saved_total` | Counter | `pipeline` | `sum(saved_topic_count)` |
| `newslab_pipeline_topics_failed_total` | Counter | `pipeline` | `sum(failed_topic_count)` |
| `newslab_pipeline_items_processed_total` | Counter | `pipeline` | `sum(extraction_runs.success_count)` |
| `newslab_pipeline_items_failed_total` | Counter | `pipeline` | `sum(extraction_runs.failed_count)` |
| `newslab_exporter_scrape_error_total` | Counter | `query` | exporter 자체 |

`pipeline` label 값: `rss_collector`, `extraction`, `three_day`, `weekly`
**`daily`는 실행 이력 table이 없어 제외한다** (`metrics.md` STEP 0-3에서 확정).

### 2. 설계 제약

- **`prometheus.Collector` 인터페이스를 직접 구현한다.** 전역 gauge를 background
  goroutine이 갱신하는 방식을 쓰지 않는다. `Describe()`와 `Collect()`만 구현하면
  인터페이스를 만족한다
- 누적값은 `prometheus.NewConstMetric(desc, prometheus.CounterValue, v, labels...)`로 노출한다.
  run table이 append-only임을 `metrics.md` STEP 0-4에서 확인했다
- 모든 DB 조회는 `context.Context`를 받고 **scrape timeout보다 짧은 timeout**을 건다
- 쿼리 6개는 `golang.org/x/sync/errgroup`으로 **동시 실행**한다.
  하나가 실패해도 어느 쿼리가 실패했는지 식별 가능해야 한다
- **조회 실패를 `0`으로 덮지 않는다.** 실패한 쿼리의 지표는 노출하지 않고
  `newslab_exporter_scrape_error_total{query="..."}`를 올린다
- `status` label 값을 하드코딩하지 않는다. DB `group by` 결과를 그대로 쓴다
- 비율을 exporter가 계산하지 않는다. 원자값만 노출한다
- label에 `article_id`, `topic_id`, URL 등 고유값을 넣지 않는다

### 3. read-only

- **연결 단계에서 강제한다.** pgx `RuntimeParams`에
  `default_transaction_read_only = on`을 설정해 커넥션 풀 전체에 적용한다.
  쿼리마다 `set transaction read only`를 실행하지 않는다 (빠뜨릴 수 있음)
- `insert`/`update`/`delete`/DDL을 코드에 두지 않는다
- 접속 계정은 read-only 권한으로 발급한다 (사람 수행).
  코드 레벨과 권한 레벨을 둘 다 건다

### 4. 설정

환경변수로만 받는다. 코드에 접속 문자열을 두지 않는다.

| 변수 | 기본값 | 설명 |
| --- | --- | --- |
| `DATABASE_URL` | (필수) | PostgreSQL 접속 문자열 |
| `LISTEN_ADDR` | `:9310` | HTTP listen 주소 (9100은 `node_exporter`가 사용) |
| `METRICS_PATH` | `/metrics` | 지표 경로 |
| `SCRAPE_TIMEOUT` | `10s` | 전체 scrape timeout |
| `DB_MAX_CONNS` | `4` | connection pool 상한 |

### 5. 컨테이너와 배포

- multi-stage Dockerfile. 최종 stage는 distroless 또는 scratch
- `GOOS=linux GOARCH=arm64` (운영 노드가 Oracle Ampere A1)
- **이미지 크기를 측정해 `docs/verification/`에 기록한다.** 기존 `seocj/news-api`(Python)와 비교
- `k8s/`에 Deployment, Service, ServiceMonitor

---

## Implementation Units

### UNIT-01. Go 프로젝트 뼈대와 `/metrics` 하드코딩 노출

- `go mod init github.com/seochanjin/news-lab-exporter`
- `main.go`: `promhttp.Handler()`로 `/metrics` 서빙
- 하드코딩한 gauge 1개를 노출해 `curl localhost:9100/metrics`로 확인
- DB 연결 없음

**완료 조건**: 로컬에서 `/metrics`에 하드코딩 값이 뜬다.

> 이 UNIT은 의도적으로 작다. Go 빌드·실행·모듈 구조를 먼저 손에 익히는 것이 목적이다.

### UNIT-02. 설정과 DB 연결 (read-only)

- `internal/config/config.go`: 환경변수 파싱, 필수값 누락 시 기동 실패
- DB 연결과 pool 설정
- connection 획득 시 `set transaction read only` 선언
- `/healthz` 추가 (DB ping 포함하지 않는다 — scrape와 분리)

**완료 조건**: 잘못된 `DATABASE_URL`로 기동 시 명확한 에러로 종료한다.

### UNIT-03. Collector 구현과 쿼리 6개

- `internal/collector/queries.go`: `metrics.md`의 쿼리 6개
- `internal/collector/collector.go`: `prometheus.Collector` 구현
  - `Describe()` — 모든 `*prometheus.Desc` 전달
  - `Collect()` — `errgroup`으로 6개 쿼리 동시 실행 → `NewConstMetric` 생성
- 쿼리 실패 시 해당 지표를 건너뛰고 `scrape_error_total{query}` 증가
- `context` timeout 적용

**완료 조건**: 로컬에서 실제 DB에 붙여 15종 지표가 `/metrics`에 뜬다.
`metrics.md`에 기록된 값과 일치한다 (`articles_collected_total` = 9613 등).

### UNIT-04. 테스트

- 쿼리 결과 → 지표 변환 로직의 단위 테스트 (fake DB 또는 인터페이스 분리)
- 쿼리 하나가 실패했을 때 나머지 지표가 정상 노출되는지 검증
- 실패한 쿼리의 지표가 `0`으로 노출되지 **않는지** 검증
- 실제 DB·외부 네트워크에 붙지 않는다

**완료 조건**: `go test ./...` 통과.

### UNIT-05. 컨테이너화

- multi-stage Dockerfile (build stage → distroless/scratch)
- `.dockerignore`
- ARM64 빌드 확인
- **이미지 크기 측정 후 기록** (기존 `news-api` Python 이미지와 비교)
- `.github/workflows/`에 ARM64 빌드 workflow (`news-lab` 방식 참고, immutable tag)

**완료 조건**: 이미지가 빌드되고 컨테이너에서 `/metrics`가 응답한다. 크기가 기록되어 있다.

### UNIT-06. Kubernetes manifest

- `k8s/deployment.yaml` — replica 1, `nodeSelector: workload=app`,
  resource requests/limits, securityContext (`allowPrivilegeEscalation: false`, `drop: ALL`)
- `k8s/service.yaml`
- `k8s/servicemonitor.yaml`
- Secret은 manifest에 값을 넣지 않고 `secretKeyRef`로 참조한다

> ⚠️ kube-prometheus-stack은 기본적으로 **자기 Helm release label이 붙은 ServiceMonitor만**
> 수집한다. `news-lab/k8s/monitoring/kube-prometheus-stack-values.yaml`의
> `serviceMonitorSelectorNilUsesHelmValues` 설정을 먼저 확인하고, 필요하면 ServiceMonitor에
> `release` label을 붙인다. 타겟이 안 올라오면 여기부터 본다.

**완료 조건**: manifest가 YAML로 파싱되고 `kubectl apply --dry-run=client`가 통과한다.
**실제 apply는 사람이 수행한다.**

### UNIT-07. 운영 반영과 검증 (사람 수행)

Agent가 실행하지 않는다. 사람이 수행하고 결과를 `docs/verification/`에 기록한다.

- read-only DB 계정 발급, Secret 생성
- 이미지 push
- `kubectl apply`
- Prometheus target `UP` 확인
- `/metrics` 실제 응답과 `metrics.md` 기록값 대조
- Grafana 패널 추가는 **`news-lab` 저장소의 별도 Task**

---

## Do not change

- `docs/design/metrics.md`의 확정된 지표 정의 — 변경이 필요하면 Task를 멈추고 사람에게 확인
- `news-lab` 저장소의 모든 파일 (Grafana 대시보드, kube-prometheus-stack values 포함)
- DB schema, migration
- 운영 Secret, `.env`, kubeconfig

---

## Test commands

```bash
gofmt -l .
go vet ./...
go build ./...
go test ./...

# 로컬 동작 확인
DATABASE_URL="..." go run . &
curl -s localhost:9100/metrics | grep '^newslab_'

# 이미지
docker build -t news-lab-exporter:local .
docker images news-lab-exporter:local --format '{{.Size}}'

# manifest
kubectl apply --dry-run=client -f k8s/

# 민감정보 검사
git grep -n -i -E "DATABASE_URL=|password|api[_-]?key|token" -- ':!docs/**'
```

---

## Acceptance criteria

### 지표

- [ ] `metrics.md`의 15종이 모두 `/metrics`에 노출된다
- [ ] `pipeline` label에 `daily`가 없다
- [ ] `status` label 값이 하드코딩되어 있지 않다
- [ ] 비율을 계산해 노출하는 지표가 없다
- [ ] 고유값(article_id 등)이 label에 없다
- [ ] 실제 값이 `metrics.md` 기록과 일치한다

### 설계

- [ ] `prometheus.Collector`를 직접 구현했다 (background goroutine 갱신 방식이 아니다)
- [ ] 누적값이 `NewConstMetric` + `CounterValue`로 노출된다
- [ ] 모든 쿼리가 `context` timeout을 받는다
- [ ] 쿼리 6개가 `errgroup`으로 동시 실행된다
- [ ] 쿼리 실패 시 해당 지표가 노출되지 않고 `scrape_error_total`이 증가한다
- [ ] 실패를 `0`으로 덮는 코드가 없다

### 안전성

- [ ] `set transaction read only`가 선언된다
- [ ] 쓰기 SQL·DDL이 코드에 없다
- [ ] 접속 문자열이 코드·manifest에 하드코딩되어 있지 않다
- [ ] SQL 문자열 결합이 없다 (placeholder bind만)

### 검증

- [ ] `gofmt -l .` 출력 없음
- [ ] `go vet ./...` 통과
- [ ] `go test ./...` 통과
- [ ] 이미지 크기가 측정되어 기록되어 있다
- [ ] `kubectl apply --dry-run=client` 통과
- [ ] 실행하지 않은 검증이 완료로 표시되지 않았다

### 문서

- [ ] `docs/verification/feature-pipeline-metrics-exporter.md` 작성
- [ ] `docs/devlog/feature-pipeline-metrics-exporter.md` 작성 — **새로 배운 Go 개념 포함**
- [ ] `docs/pr/feature-pipeline-metrics-exporter.md` 작성
- [ ] README 작성 (환경변수, 로컬 실행, 지표 목록)

---

## Checklist

- [x] UNIT-01 Go 뼈대와 `/metrics` 하드코딩
- [x] UNIT-02 설정과 DB 연결 (read-only)
- [x] UNIT-03 Collector 구현과 쿼리 7개 (03c 동시 실행 포함)
- [x] UNIT-04 테스트
- [ ] UNIT-05 컨테이너화와 이미지 크기 측정
- [ ] UNIT-06 Kubernetes manifest
- [ ] UNIT-07 운영 반영과 검증 (사람 수행)

---

## Notes

- **WIP 1.** 한 UNIT의 조사 → 변경 → 문서화 → 검증 → checklist 갱신을 끝내기 전에
  다음 UNIT으로 넘어가지 않는다
- 구현자는 Go가 처음이다. 새 개념(인터페이스 암묵 구현, `defer`, 리시버, `errgroup`)이
  나오면 코드를 생성하기 전에 설명하고, `docs/devlog/`에 기록한다
- 라이브러리 예제를 그대로 복사하지 않는다. 어느 부분이 예제이고 어느 부분이 이 프로젝트의
  판단인지 구분해 남긴다
- Grafana 패널 추가와 `serviceMonitorSelector` 확인은 `news-lab` 저장소의 별도 Task다
- 후속 과제(LLM 토큰·비용, daily run table, 파이프라인 간 상태 어휘 통일, stage별 duration)는
  `docs/design/metrics.md`의 "미결정 / 후속 과제" 표를 따른다. 이번 Task에서 처리하지 않는다
