# Devlog: NewsLab pipeline metrics exporter (Go) — UNIT-01~03

## 작업 목적

Kubernetes Job은 process exit code에 따른 성공/실패만 보여 준다.
`news-lab/docs/design/pipeline-operations-dashboard.md`의 UNIT-06에서 이 한계로 확인할 수 없는
업무 metric 8개를 이미 문서로 확정해 두었고, 그때 "metric 이름·label cardinality·수집 경로·
retention은 별도 Task에서 설계해야 한다"고 남겼다. 이 작업이 그 후속이다.

## 기존 문제

- 파이프라인이 `partial_success`로 끝나도 Job은 성공으로 보인다
- 후보 건수, 임베딩 재사용/미보유, 저장 성공/실패 토픽 수를 Job exit code로 복원할 수 없다
- 이력서에 쓰던 "6,300여 건 수집", "중복 0건" 같은 수치가 전부 사후 수동 집계였다

## 왜 Go인가

**exporter는 별도 프로세스가 표준이다.** `postgres_exporter`, `sql_exporter` 모두 그렇다.
조회 API에 `/metrics`를 붙이면 지표 노출이 API 가용성에 묶이고 배포 주기도 함께 묶인다.

별도 프로세스로 만들기로 하면 언어 선택이 열린다. Prometheus 자체가 Go로 쓰여 있고
`prometheus/client_golang`이 가장 성숙하므로 Go를 택했다.
`학습_Go-왜-쓰는가.md`에 정리한 판단 근거와 같다.

## 왜 scrape 시점에 DB를 조회하는가

`prometheus.Collector` 인터페이스를 직접 구현했다. 전역 gauge를 background goroutine이
주기적으로 갱신하는 방식도 가능하지만 택하지 않았다.

- 갱신 주기와 scrape 주기가 어긋나면 **오래된 값을 최신인 것처럼 내보내게 된다**
- 상태를 들고 있으면 동시성 제어가 필요해진다
- Prometheus는 Pull 모델이므로 "물어볼 때 답한다"가 모델과 일치한다

## 왜 read-only를 연결 단계에서 거는가

```go
connCfg.RuntimeParams["default_transaction_read_only"] = "on"
```

쿼리마다 `set transaction read only`를 실행하는 방법도 있으나, **빠뜨릴 수 있다.**
커넥션 파라미터로 걸면 커넥션 풀이 새 연결을 만들어도 자동 적용된다.

권한 레벨(read-only 역할)과 코드 레벨을 **둘 다** 건다. 하나가 뚫려도 다른 하나가 막는다.
read-only 역할 발급은 UNIT-07의 사람 수행 항목으로 남겼다.

## 구현 중 잡은 결함 — `rows.Err()`

최초 구현에서 다중 행 조회의 반복문 뒤에 `rows.Err()`를 확인하지 않았다.

반복 중간에 조회가 끊기면 `rows.Next()`가 `false`를 반환하고 루프는 **정상 종료된 것처럼**
빠져나온다. 그러면 부분 데이터만 받은 상태로 `query_success = 1`을 내보내게 된다.

**이건 이 exporter가 드러내려는 문제와 정확히 같은 패턴이다.**
`extraction_runs`는 항목이 47건 실패해도 run status를 `success`로 기록한다.
그 문제를 지적하려고 만드는 프로그램이 같은 실수를 하면 안 된다.

모든 다중 행 조회에서 `rows.Err()`를 확인하도록 수정했다.

## 값이 없는 것과 0을 구분한다

`last_success_timestamp`에 `sql.NullFloat64`를 썼다.

Go의 `float64`는 DB의 `NULL`을 표현할 수 없어 `0`이 된다. 그런데 이 지표에서 `0`은
1970-01-01을 뜻하므로, **"한 번도 성공한 적 없음"과 "아주 오래전에 성공함"이 구분되지 않는다.**

`Valid`가 `false`면 지표를 아예 내보내지 않기로 했다.
`news-lab` Grafana dashboard에서 `or vector(0)`을 거부한 것과 같은 판단이다.

## 계약 변경

`newslab_exporter_scrape_error_total` (Counter) → `newslab_exporter_query_success` (Gauge).

Counter는 값을 누적해 기억해야 하는데 `Collector`는 무상태 구조라 뮤텍스와 별도 registry가
필요해진다. `node_exporter`의 `node_scrape_collector_success`와 같은 형태로 바꿨다.
무상태로 구현되고 알림 규칙도 `== 0`으로 단순하다.

## 처음 쓰면서 이해한 Go 개념

| 개념 | 이해한 내용 |
| --- | --- |
| 이름 먼저, 타입 나중 | `db *sql.DB`는 이름이 `db`, 타입이 `*sql.DB`. Java와 반대라 처음에 안 읽혔다 |
| 메서드 리시버 | `func (c *Collector) Collect(...)`의 앞 괄호가 "이 함수의 주인". Python `self` 자리 |
| 인터페이스 암묵 구현 | `implements` 선언이 없다. `Describe`/`Collect` 두 메서드를 가진 것만으로 `prometheus.Collector`가 된다 |
| 에러는 반환값 | 예외가 없다. `err`를 받아 `if err != nil`로 검사한다 |
| `defer` | 여는 줄 바로 아래에 닫는 줄을 둔다. 중간 `return`이 몇 개든 실행된다 |
| `chan<-` | 보내기 전용 채널. Collector가 값을 만드는 동안 라이브러리가 동시에 받아갈 수 있다 |
| `Desc` vs `Metric` | 이름표는 `New()`에서 한 번, 값은 `Collect()`에서 매번 |
| `go mod tidy` | import를 스캔해 `go.mod`/`go.sum`을 실제 코드와 맞춘다. import 변경 후 습관적으로 실행해야 한다 |

## 겪은 문제

**`go.mod`의 `// indirect`로 빌드 실패** — `go get`을 import 작성 전에 실행해서 Go가 pgx를
간접 의존으로 기록했다. `go mod tidy`로 해결.

**`DATABASE_URL` 형식 불일치** — `news-lab`은 SQLAlchemy 형식(`postgresql+psycopg://`)을 쓴다.
pgx는 이를 해석하지 못한다. exporter 저장소의 `.env`에는 표준 형식으로 따로 기록했다.
같은 DB를 언어별로 다르게 부르는 것이 정상이므로 통일하지 않았다.

## 가동 후 확인된 것

만 하루 만에 조사 단계의 발견이 **회고가 아니라 진행 중인 문제**임이 드러났다.

- `three_day`가 8/8·8/9·8/10 **3연속 `partial_success`**. `success` 카운트가 32에서 멈췄고
  마지막 완전 성공은 82.8시간 전이다
- `weekly`도 8/10 실행분이 `partial_success`. 마지막 성공은 183시간 전
- `extraction` 실패 항목이 47 → 50으로 계속 쌓이는데 run status는 여전히 전부 `success`

**셋 다 Kubernetes Job에서는 초록불이다.** 기존 대시보드로는 지금도 정상으로 보인다.

`last_success_timestamp`가 생기면서 신선도 SLO를 정의할 근거가 마련됐다.

```promql
time() - newslab_pipeline_last_success_timestamp_seconds{pipeline="three_day"} > 26*3600
```

## 동시 실행 — `errgroup`이 아니라 `WaitGroup`

Task 문서에는 `errgroup`이라고 적어뒀지만 구현하면서 바꿨다.

`errgroup`이 주는 건 에러 전파, `WithContext` 취소 전파, 동시성 상한 셋이다.
우리는 각 함수가 `query_success=0`으로 실패를 자체 처리하므로 에러 전파가 필요 없고,
**`WithContext`의 취소 전파는 오히려 요구와 반대다.** 쿼리 하나가 실패해도
나머지 지표는 나와야 하기 때문이다.

에러를 전파하지 않는데 `errgroup`을 쓰면 전 함수가 `return nil`만 하게 된다.
필요해서가 아니라 관례로 쓰는 것이 되므로 `sync.WaitGroup`을 택했다.

## 동시 실행이 드러낸 장애

순차 실행에서 7개 모두 성공하던 쿼리가, 동시 실행으로 바꾸자 매 scrape마다
**무작위로 5~6개가 실패**했다. 성공하는 쿼리가 매번 달랐다는 점에서
특정 쿼리의 문제가 아니라 공유 자원 경쟁임을 알 수 있었다.

그런데 당시 코드로는 **원인을 알 수 없었다.** `err`를 받아 버리고
`query_success=0`만 내보내고 있었기 때문이다. 실패 사실은 알지만 이유는 몰랐다.

`reportQueryFailure` 헬퍼로 로그를 남기도록 고치자 한 줄로 확정됐다.

```
prepared statement "stmtcache_19b8..." does not exist (SQLSTATE 26000)
```

Supabase transaction pooler는 트랜잭션마다 backend connection을 재배정한다.
pgx 기본값인 extended protocol은 PREPARE한 backend와 EXECUTE하는 backend가
달라지면 실패한다. 순차 실행에서는 커넥션 하나를 재사용해 드러나지 않았고
동시 실행이 방아쇠가 됐다.

`DefaultQueryExecMode`를 simple protocol로 바꾸고 `DB_MAX_CONNS`(기본 4) 상한을
추가해 해결했다. **환경 제약에 맞춰 클라이언트 동작을 조정한 것이지 성능 최적화가 아니다.**

한 가지 부수 효과가 있었다. 장애 중에 **실패한 쿼리의 업무 지표는 하나도 노출되지 않고
성공한 쿼리의 지표만 노출됐다.** "하나가 실패해도 나머지는 내보낸다"는 설계가
실제 장애에서 의도대로 동작함이 우연히 검증됐다.

## 테스트 — 무엇을 고정했는가

`go-sqlmock`으로 `*sql.DB`를 대체했다. **실제 DB로는 "연결이 끊긴 상황"을 만들 수 없기 때문이다.**

| 테스트 | 고정한 원칙 |
| --- | --- |
| `쿼리가_실패하면_업무지표를_내보내지_않는다` | 실패를 `0`으로 덮지 않는다. 위 장애에서 확인한 동작을 회귀 방지로 고정 |
| `성공이력이_없으면_지표를_생략한다` | `NULL`과 `0`을 구분한다 |
| `getEnvInt` 5케이스 | 잘못된 설정으로 반쯤 동작하는 상태를 만들지 않는다 |

실패 테스트를 돌리면 `collector query failed: ... err=연결 끊김` 로그가 출력된다.
실패 경로가 실제로 실행됐다는 증거다.

커버리지를 넓히기보다 **판단이 담긴 지점을 고정하는 데 집중했다.**
나머지 5개 collect 함수는 `collectCrawlRuns`와 구조가 같아 대표 케이스만 검증했다.

## 남은 작업

- read-only 동작의 실제 거부 확인 — 모든 쿼리가 `select`라 아직 확인 기회가 없었다.
  UNIT-07 운영 반영 시 사람이 확인한다
- 컨테이너화와 이미지 크기 측정, Kubernetes manifest
- `Collect()` 통합 테스트
- Grafana 패널 추가는 `news-lab` 저장소의 별도 Task

## 후속 조사 대상 (exporter 범위 아님)

- `three_day` 3연속 `partial_success`의 원인 — `failed_topic_count` 발생 이유
- `rss_collector` 마지막 성공이 8/10 15:53 KST인데 CronJob 스케줄은 03:00 KST다
- 파이프라인 간 상태 판정 어휘 통일 (`extraction`만 부분 실패를 `success`로 기록)
