# Verification: NewsLab pipeline metrics exporter (Go)

## Verification Status

pending

UNIT-01, UNIT-02 완료. UNIT-03~07 미수행.

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

## 미수행

- UNIT-03 Collector 구현과 쿼리 6개
- UNIT-04 테스트
- UNIT-05 컨테이너화, 이미지 크기 측정
- UNIT-06 Kubernetes manifest
- UNIT-07 운영 반영 (사람 수행)
- read-only 권한 계정 발급 (사람 수행)
- read-only 동작의 실제 거부 확인 (UNIT-03에서 수행)
- `gofmt` / `go vet` / `go test` 미실행

## 사람이 수행할 항목

- Supabase에서 read-only 역할 발급 및 Secret 생성
- 운영 K3s 적용, 이미지 push
- Prometheus target `UP` 확인
- Grafana 패널 추가 (`news-lab` 저장소의 별도 Task)
