# AGENTS.md

## 프로젝트

`news-lab-exporter`는 NewsLab의 PostgreSQL/Supabase 실행 이력 table을 읽어
Prometheus 지표로 노출하는 Go exporter다. 운영 환경은 Oracle Cloud A1 node의
K3s cluster이며 kube-prometheus-stack이 scrape한다.

**이 exporter가 존재하는 이유**는 `news-lab` 저장소의
`docs/design/pipeline-operations-dashboard.md` "Kubernetes metric으로 확인할 수 없는
업무 metric" 절에 이미 확정되어 있다. Kubernetes Job은 process exit code에 따른
성공/실패만 보여 주므로 `partial_success`, candidate/embedding count, saved/failed
topic count를 복원할 수 없다. 그 공백을 메우는 것이 이 저장소의 범위다.

## 절대 규칙 — read-only

- **DB에 쓰지 않는다.** `insert`, `update`, `delete`, `create`, `alter`, `drop` 금지.
- **연결 단계에서 read-only를 강제한다.** 쿼리마다 `set transaction read only`를
  실행하지 않는다. 빠뜨릴 수 있기 때문이다.

  ```go
  connCfg, err := pgx.ParseConfig(databaseURL)
  connCfg.RuntimeParams["default_transaction_read_only"] = "on"
  db := stdlib.OpenDB(*connCfg)
  ```

  커넥션 풀이 새 연결을 만들어도 자동 적용된다.
- schema migration은 이 저장소의 범위가 아니다. 필요하면 `news-lab` 쪽 후속 task로 남긴다.
- DB 계정은 read-only 권한으로 발급한다 (사람 수행).
  **코드 레벨(`default_transaction_read_only`)과 권한 레벨(read-only 역할)을 둘 다 건다.**

## 접속 문자열 형식

`news-lab`의 `.env`는 SQLAlchemy 형식(`postgresql+psycopg://`)을 쓴다.
**pgx는 이 형식을 해석하지 못한다.** 이 저장소의 `.env`에는 표준 형식으로 기록한다.

```
DATABASE_URL=postgresql://user:password@host:port/dbname
```

두 저장소의 값을 통일하지 않는다. 언어가 다르면 형식이 다른 것이 정상이다.

## 안전 규칙

- `main`에 직접 push하지 않는다.
- 명시적 요청 없이 `git push`, `git merge`를 실행하지 않는다.
- `kubectl apply/delete/patch/edit/rollout`, Helm 변경, `docker push`를 실행하지 않는다.
- secret, `.env`, kubeconfig, credential, token을 수정하거나 값을 문서에 기록하지 않는다.
- production verification은 사람이 제공한 실제 결과 없이 완료로 표시하지 않는다.
- 실제 DB 접속 정보로 운영 DB에 붙는 실행은 사람이 수행한다.
- 변경은 작고 review 가능하게 유지한다.

## Go 구현 규칙

- module path는 `github.com/seochanjin/news-lab-exporter`.
- 진입점은 `main.go` 하나. 설정 파싱 → DB 연결 → collector 등록 → HTTP 서버까지만 담당한다.
- 도메인 코드는 `internal/` 아래에 둔다. 외부에서 import할 라이브러리가 아니다.
  - `internal/collector/` — `prometheus.Collector` 구현과 SQL
  - `internal/config/` — 환경변수 파싱
- **지표는 `prometheus.Collector` 인터페이스를 직접 구현해 scrape 시점에 조회한다.**
  전역 gauge를 background goroutine이 갱신하는 방식을 쓰지 않는다.
  (`Describe()`와 `Collect()` 두 메서드만 구현하면 인터페이스를 만족한다)
- 모든 DB 조회는 `context.Context`를 받고 scrape timeout보다 짧은 timeout을 건다.
- 여러 쿼리는 `golang.org/x/sync/errgroup`으로 동시 실행한다.
  하나가 실패해도 어느 쿼리가 실패했는지 식별 가능해야 한다.
- SQL은 문자열 결합하지 않는다. placeholder bind만 사용한다.
- error를 삼키지 않는다. `if err != nil`에서 원인을 감싸서 올린다 (`fmt.Errorf("...: %w", err)`).
- 설정은 환경변수로만 받는다. 코드에 접속 문자열을 두지 않는다.

## 지표 규칙

- 이름은 `newslab_` prefix + 단수 명사 + 단위 suffix (Prometheus 명명 관례).
  - 누적값은 `_total`, 초 단위는 `_seconds`, byte는 `_bytes`
- label cardinality를 늘리지 않는다. `article_id`, `topic_id`, URL 같은 고유값을 label로 쓰지 않는다.
- **scrape 실패와 정상 0을 구분한다.** 조회가 실패하면 해당 지표를 노출하지 않고
  `newslab_exporter_scrape_error_total`을 올린다. 실패를 `0`으로 덮지 않는다.
  (이 원칙은 `news-lab` Grafana dashboard에서 `or vector(0)`을 거부한 판단과 같다)
- 새 지표를 추가할 때는 **왜 이 지표가 Kubernetes metric으로 대체 불가능한지**를
  `docs/`에 함께 기록한다.

## 검증 원칙

`news-lab` 저장소의 문서 문화를 그대로 따른다.

- Task 문서에는 checklist가 있어야 하고, **실제 완료한 항목만** 체크한다.
- 실행한 command와 실제 결과만 verification 문서에 기록한다.
- 미수행 / 환경 제약 실패 / 운영 반영 후 확인 필요 / 사람이 수행 필요를 구분한다.
- 자동 test·lint가 없는 영역을 통과한 것처럼 쓰지 않는다.
- 최소 검증: `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .`

## Workflow artifact

| 목적 | 경로 |
| --- | --- |
| Task source of truth | `docs/tasks/` |
| 실제 검증 기록 | `docs/verification/` |
| PR draft | `docs/pr/` |
| Devlog draft | `docs/devlog/` |
| 설계 근거 | `docs/design/` |

## 주요 구성

- 진입점: `main.go`
- Collector: `internal/collector/`
- 설정: `internal/config/`
- 컨테이너: `Dockerfile` (multi-stage → distroless 또는 scratch)
- 배포: `k8s/` (Deployment, Service, ServiceMonitor)

## 관련 저장소

| 저장소 | 역할 |
| --- | --- |
| `news-lab` | FastAPI backend. **이 exporter가 읽는 table의 소유자** |
| `news-lab-web` | Next.js frontend |
| `news-lab-exporter` | 이 저장소 |

## ⚠️ schema source of truth

**`news-lab/db/migrations/`는 완전하지 않다.** 일부 table은 Supabase 콘솔에서 직접
생성되어 migration file이 없다. migration file만 보고 컬럼을 가정하면 틀린다.

schema는 **반드시 실제 DB 조회로 확인**한다.

```sql
select table_name from information_schema.tables
where table_schema = 'public' order by table_name;

select column_name, data_type, is_nullable, column_default
from information_schema.columns
where table_schema = 'public' and table_name = '<TABLE>'
order by ordinal_position;
```

확인 결과는 `docs/design/metrics.md`에 실제 출력 그대로 기록한다.
migration file은 참고 자료로만 쓴다.
