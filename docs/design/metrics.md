# 지표 설계 — 조사 기록

> SQL을 실행하고 결과를 그대로 붙인 뒤 판단을 남긴 문서다.
> 추측으로 채운 칸은 없다. 미수행 항목은 명시했다.

## 조사 상태

| 항목 | 값 |
| --- | --- |
| Status | `완료` (STEP 0~4). 지표 정의 확정됨 |
| 조사일 | 2026-08-09 |
| 조사 환경 | 운영 Supabase (Supabase SQL Editor) |
| 접속 권한 | 조사는 사람이 직접 수행. exporter용 read-only 계정 발급은 **미수행** |

---

## 왜 이 exporter인가

`news-lab/docs/design/pipeline-operations-dashboard.md`의
"Kubernetes metric으로 확인할 수 없는 업무 metric" 절에서 UNIT-06이 이미 확정한 공백이다.

| # | 공백 | 대체 불가 이유 (원문) |
| --- | --- | --- |
| ① | Pipeline `partial_success` | Job 성공/실패는 일부 Topic 저장 성공과 상세 상태를 구분하지 못함 |
| ② | DB run table last success·상세 status | kube-state-metrics는 application DB record를 읽지 않음 |
| ③ | candidate count | Pod/Job resource metric에 선정 대상 건수가 포함되지 않음 |
| ④ | embedding created/reused/missing count | CPU/Memory는 embedding 처리 결과를 구분하지 못함 |
| ⑤ | saved topic count | Job exit code로 저장된 Topic 수를 복원할 수 없음 |
| ⑥ | failed topic count | Job exit code로 저장 실패 Topic 수를 복원할 수 없음 |
| ⑦ | Pipeline stage별 duration | Job 전체 시간과 stage별 소요 시간은 다름 |
| ⑧ | Summary provider 오류 수 | container waiting/restart는 provider API 오류를 보여 주지 않음 |

같은 문서에 **"metric 이름, label cardinality, 수집 경로와 retention은 별도 Task에서
설계해야 한다"**고 남겼다. 이 문서가 그 Task다.

이번 범위는 ①②③④⑤⑥이다. ⑦⑧은 후속 과제로 남긴다.

---

## ⚠️ 스키마 source of truth

`news-lab/db/migrations/`는 **완전하지 않다.** 일부 table은 Supabase 콘솔에서 직접
생성되어 migration file이 없다. 모든 스키마는 실제 DB 조회로 확인했다.

---

# STEP 0 — 실제 스키마 확인

## 0-1. 전체 table 목록

```sql
select table_name from information_schema.tables
where table_schema = 'public' order by table_name;
```

**실행 결과**

```text
article_embeddings / articles / crawl_runs / extraction_runs / raw_articles / sources
three_day_topic_articles / three_day_topic_runs / three_day_topics
topic_articles / topics
weekly_topic_articles / weekly_topic_runs / weekly_topics
```

**확인 사항**

- [x] migration file에 없는 table이 있는가? → **`articles`, `sources`** (migration에 생성문 없음, Supabase 직접 생성)
- [x] daily topic pipeline의 실행 이력 table이 존재하는가? → **없음**
- [x] `articles`, `sources` 존재 확인 → 존재함

## 0-2. 실행 이력(run) 계열 table 탐색

```sql
select table_name from information_schema.tables
where table_schema = 'public'
  and (table_name like '%run%' or table_name like '%log%' or table_name like '%history%')
order by table_name;
```

**실행 결과**

```text
crawl_runs / extraction_runs / three_day_topic_runs / weekly_topic_runs
```

## 0-3. daily 실행 이력 table

**실행 결과**

```text
Success. No rows returned
```

**판단**

```text
daily 포함 여부: 제외
이유: daily topic pipeline은 실행 이력 table이 없다. 0-1 목록과 0-2 탐색 모두에서
      확인했다. 3day(007)·weekly(008)는 run table을 함께 만들었으나 daily(005)는
      그 이전이라 빠졌다.
      → daily의 status·last success·candidate/saved/failed count는 이번 exporter로
        노출할 수 없다. 후속 과제로 기록한다.
      → 단, daily가 생성하는 embedding의 누적량은 article_embeddings의 row 수로
        간접 노출한다 (M2 참조).
```

## 0-4. Counter 안전성

```sql
select 'three_day' as t, min(id), max(id), count(*) from three_day_topic_runs
union all select 'weekly', min(id), max(id), count(*) from weekly_topic_runs
union all select 'crawl',  min(id), max(id), count(*) from crawl_runs;
```

**실행 결과**

```text
| t         | min | max | count |
| three_day | 1   | 49  | 49    |
| weekly    | 1   | 9   | 9     |
| crawl     | 1   | 73  | 73    |
```

**확인 사항**

- [x] `max(id) - min(id) + 1` 과 `count(*)` 가 일치하는가? → **세 table 모두 일치. 삭제 이력 없음**
- [x] 오래된 run을 정리하는 절차나 CronJob이 있는가? → **없음** (append-only)

**판단**

```text
삭제 이력: 없음
Counter로 노출해도 안전한가: 안전하다.
  DB 누적합이 단조증가하므로 prometheus.CounterValue로 노출한다.
  exporter 재시작에도 값이 유지된다 (postgres_exporter와 같은 방식).
미수행: extraction_runs(252건)의 id 연속성은 확인하지 않았다. 같은 패턴으로 추정하나
        확인 전까지 단정하지 않는다.
```

## 0-5. `crawl_runs.status` 실제 값

**실행 결과**

```text
| status  | count |
| success | 73    |
```

**판단**

```text
crawl_runs는 success 하나뿐. 3day/weekly의 4상태 어휘와 다르다.
→ 정규화하지 않고 DB group by 결과를 그대로 label로 노출한다.
  exporter가 상태 어휘를 재해석하면 원본 의미를 잃는다.
```

---

# STEP 1~4 — 지표별 조회

## M1. 수집 건수

```sql
select coalesce(sum(inserted_count), 0) as collected,
       coalesce(sum(skipped_count), 0)  as skipped,
       count(*) as runs, min(started_at) as first_run, max(finished_at) as last_finish
from crawl_runs;
```

**실행 결과**

```text
| collected | skipped | runs | first_run                     | last_finish                   |
| 9613      | 3892    | 73   | 2026-06-01 09:32:14.825427+00 | 2026-08-08 18:00:56.418709+00 |
```

```sql
select count(*) as articles, min(created_at), max(created_at) from articles;
```

```text
| articles | min                           | max                           |
| 9629     | 2026-05-28 15:45:46.175081+00 | 2026-08-08 18:00:50.977539+00 |
```

**확인 사항**

- [x] `collected`가 이력서의 "6,300여 건"과 맞는가? → **아니다. 9,613건.**
      2026.07 기준 수치가 낡은 것이며 틀린 것이 아니다. **이력서 갱신 필요.**
- [x] `articles`(9,629)와 `collected`(9,613)의 16건 차이 → **crawl_runs 도입(6/1) 이전 기사.**
      `articles` min은 5/28, `crawl_runs` first_run은 6/1로 4일 차이.
      월별 커버리지 조회에서 5월 기사가 정확히 16건으로 확인되어 교차 검증됨.
- [x] `skipped_count`의 의미 → 정규화 URL 기준 중복 스킵.
      총 시도 13,505건 중 3,892건(28.8%)이 중복으로 차단됨.

**판단**

```text
status 필터: 적용하지 않는다 (crawl_runs는 success뿐).
두 값을 모두 노출한다. 의미가 다르기 때문이다.
  - sum(inserted_count) = 수집 파이프라인이 넣은 건수 → Counter
  - count(articles)     = 현재 저장된 기사 수         → Gauge (삭제 시 감소)
하나로 합치면 16건 차이의 원인을 나중에 다시 조사하게 된다.
```

---

## M2. 임베딩 후보 / 재사용 / 미보유

```sql
select 'three_day' as pipeline,
       coalesce(sum(candidate_count),0), coalesce(sum(embedding_count),0),
       coalesce(sum(missing_embedding_count),0), count(*)
from three_day_topic_runs
union all
select 'weekly', coalesce(sum(candidate_count),0), coalesce(sum(embedding_count),0),
       coalesce(sum(missing_embedding_count),0), count(*)
from weekly_topic_runs;
```

**실행 결과**

```text
| pipeline  | candidates | reused | missing | runs |
| three_day | 20713      | 20129  | 584     | 49   |
| weekly    | 8764       | 7044   | 1720    | 9    |
```

```sql
select count(*) as total,
       count(distinct (article_id, provider, model, source_text_type)) as distinct_keys
from article_embeddings;
```

```text
| total | distinct_keys |
| 7061  | 7061          |
```

### ⚠️ 컬럼 의미 정정

`README.md` 기준:

> 3일 토픽은 최근 72시간 기사와 **기존 `article_embeddings`만 읽어** 재클러스터링한다
> 일간 토픽은 최근 24시간 기사 후보의 embedding을 **생성하거나 재사용**한다

| 컬럼 | 실제 의미 |
| --- | --- |
| `embedding_count` | 기존 임베딩을 **재사용**한 후보 수 |
| `missing_embedding_count` | 임베딩이 **없어서 사용하지 못한** 후보 수 (신규 생성이 아니다) |

**3day/weekly는 임베딩을 생성하지 않는다.** 생성은 daily가 하는데 daily는 run table이 없다.
→ 공백 ④의 `created`는 run table로 노출 불가. `count(article_embeddings)`로 대체한다.

**확인 사항**

- [x] 재사용률 → three_day **97.2%**, weekly **80.4%**, 전체 **92.2%** (27,173 / 29,477)
- [x] 임베딩 1건당 평균 사용 횟수 → **3.85회** (27,173 / 7,061)
- [x] 중복 생성 여부 → **total 7,061 = distinct_keys 7,061. 중복 0건 실증됨**
- [x] weekly 재사용률이 낮은 이유 → 7일 창이 3일 창보다 오래된 기사를 더 포함하고,
      오래된 기사일수록 임베딩 보유율이 낮다 (아래 커버리지 표 참조)

**판단**

```text
status 필터: 적용하지 않는다. running/failed 상태의 run이 0건이므로 차이가 없고,
             필터를 넣으면 미래에 failed가 생겼을 때 누락된다.
비율 노출: 하지 않는다. 원자값(candidates/reused/missing)만 노출하고
           비율은 Grafana/PromQL에서 계산한다 (Prometheus 관례).
```

### 임베딩 커버리지 분해 ★

```sql
select date_trunc('month', a.created_at) as month, count(*) as articles,
       count(e.id) as with_embedding,
       round(100.0 * count(e.id) / count(*), 1) as coverage_pct
from articles a left join article_embeddings e on e.article_id = a.id
group by 1 order by 1;
```

```text
| month   | articles | with_embedding | coverage_pct |
| 2026-05 | 16       | 0              | 0.0          |
| 2026-06 | 3779     | 1667           | 44.1         |
| 2026-07 | 4652     | 4318           | 92.8         |
| 2026-08 | 1182     | 1076           | 91.0         |
```

전체 커버리지 73.3%(7,061 / 9,629)는 **오해를 부르는 수치다.** 결손 2,568건을 분해하면:

| 구간 | 결손 | 성격 |
| --- | --- | --- |
| 5월 | 16 | crawl_runs·임베딩 도입 이전 |
| 6월 | **2,112 (82%)** | 임베딩 파이프라인 6월 중순 도입 → 이전분 백필 미수행 |
| 7월 | 334 | 정상 운영 중 발행 지연분 |
| 8월 | 106 | 정상 운영 중 발행 지연분 |

**정상 운영 커버리지는 92%이고, 나머지 8%는 설계상 제외되는 기사다.**

### 결손 원인 확인

```sql
select case
    when a.published_at is null then 'published_at 없음'
    when a.created_at - a.published_at > interval '24 hours' then '24h 초과 지연'
    when a.created_at - a.published_at > interval '1 hour'  then '1~24h 지연'
    else '1h 이내' end as bucket,
  count(*)
from articles a left join article_embeddings e on e.article_id = a.id
where e.id is null and a.created_at > now() - interval '14 days'
group by 1 order by 2 desc;
```

```text
| bucket      | count |
| 24h 초과 지연 | 122   |
| 1~24h 지연   | 36    |
```

**최근 14일 결손 158건 = 122 + 36으로 정확히 일치한다.**
`published_at 없음`과 `1h 이내`는 0건이다.

**판단**

```text
결손 원인: 버그가 아니라 설계다.
  daily pipeline의 창은 coalesce(published_at, created_at) 기준 24시간이다.
  RSS 피드가 며칠 지난 기사를 서빙하면 오늘 수집돼도 발행일이 창 밖이라
  임베딩 대상에서 제외된다. 최근 결손의 100%가 이 경우다.
  --max-articles 300은 원인이 아니다 (일 수집 130~180건으로 미도달).

일별 결손률은 4~16%로 변동하나 추세적 악화로 단정할 근거는 없다 (08-01에도 14.3% 스파이크).
```

---

## M3. 실행 상태 / 마지막 성공 시각 ★ 핵심 지표

**실행 결과 — 상태별 실행 수**

```text
| pipeline      | status          | runs |
| rss_collector | success         | 73   |
| three_day     | partial_success | 17   |
| three_day     | success         | 32   |
| weekly        | partial_success | 1    |
| weekly        | success         | 8    |
```

**실행 결과 — 마지막 성공 시각**

```text
| pipeline      | last_success_at               | last_success_epoch |
| three_day     | 2026-08-06 20:06:14.298005+00 | 1786046774.298005  |
| weekly        | 2026-08-02 15:38:16.817905+00 | 1785685096.817905  |
| rss_collector | 2026-08-08 18:00:56.418709+00 | 1786212056.418709  |
```

**실행 결과 — three_day 최근 5건**

```text
| id | reference_date | status          | candidate | reused | missing |
| 45 | 2026-08-05     | partial_success | 390       | 386    | 4       |
| 46 | 2026-08-06     | success         | 446       | 440    | 6       |
| 47 | 2026-08-07     | success         | 480       | 468    | 12      |
| 48 | 2026-08-08     | partial_success | 479       | 462    | 17      |
| 49 | 2026-08-09     | partial_success | 429       | 404    | 25      |
```

**실행 결과 — 상태별 실패 토픽 대응**

```text
| status          | runs | failed_topics |
| partial_success | 17   | 19            |
| success         | 32   | 0             |
```

**실행 결과 — finished_at null**

```text
three_day 0 / weekly 0 / crawl 0
```

**확인 사항**

- [x] `partial_success`가 실제로 발생하는가? → **three_day 17/49 = 34.7%, weekly 1/9 = 11.1%**
- [x] `running`으로 남은 오래된 run이 있는가? → **없음.** `finished_at` null도 0건
- [x] 마지막 성공 시각이 스케줄과 일치하는가? → **three_day는 8/8, 8/9 두 번 연속 `partial_success`.**
      window 시각이 정확해 실행 자체는 정시에 되고 있다. Kubernetes Job은 초록불이다.
- [x] 상태 판정 로직의 일관성 → **`success`인데 failed_topic이 있는 경우 0건.**
      "토픽이 하나라도 저장 실패하면 partial_success" 규칙이 49회 내내 지켜졌다.
      → 상태 어휘를 label로 써도 신뢰 가능하다.

**판단**

```text
status label 값: 하드코딩하지 않는다. DB group by 결과를 그대로 노출한다.
  현재는 success/partial_success 둘뿐이나 미래에 failed가 생기면 자동으로 잡혀야 한다.
last_success 없는 pipeline: 지표를 노출하지 않는다.
  0을 노출하면 1970-01-01로 표시되어 "오래됨"과 "한 번도 성공 안 함"을 구분할 수 없다.
running 상태: 현재 0건이나 label에서 배제하지 않는다.
```

---

## M4. 저장 성공 / 실패 토픽 수

**실행 결과**

```text
| pipeline  | clusters | selected | saved | failed |
| three_day | 17016    | 245      | 226   | 19     |
| weekly    | 5782     | 39       | 37    | 2      |
```

**확인 사항**

- [x] `saved + failed = selected` 정합성 → **three_day 226+19=245 ✓, weekly 37+2=39 ✓**
- [x] 저장 실패율 → three_day **7.8%** (19/245), weekly **5.1%** (2/39)
- [x] `clusters` 노출 가치 → three_day 17,016 클러스터 중 245개만 selected.
      대부분 singleton으로 추정되며 이는 클러스터링 품질 평가(로드맵 Week 3) 영역이다.
      **이번 지표에는 넣지 않는다.**

**판단**

```text
노출할 값: selected / saved / failed. clusters는 제외.
selected를 함께 내는 이유: saved 단독으로는 "선정 대비 저장 성공률"을 계산할 수 없다.
```

---

## M5. 원문 추출 (extraction_runs)

**실행 결과**

```text
| status  | runs | extracted | failed | last_finish                   |
| success | 252  | 657       | 47     | 2026-08-08 20:01:16.399763+00 |
```

**확인 사항**

- [x] 추출 비율 → **657 / 9,629 = 6.8%.** 이력서의 "7.4%만 원문·LLM 처리"와 정합
- [x] 실패율 → **47 / 704 = 6.7%**
- [x] 상태 어휘 → **252회 전부 `success`인데 실패가 47건 있다**

**판단 ★ 중요**

```text
extraction_runs는 3day/weekly와 상태 판정 규칙이 다르다.
  3day/weekly : 토픽 하나만 실패해도 partial_success (49회 실행에서 검증됨)
  extraction  : 47건 실패해도 전부 success

의도된 설계인지 일관성 결함인지는 이 조사로 판단할 수 없다.
그러나 지표 설계에는 직접 영향이 있다.

→ status label만 노출하면 47건의 실패가 완전히 숨는다.
  newslab_pipeline_items_failed_total{pipeline="extraction"}을 반드시 별도로 노출한다.

후속 확인 항목: 파이프라인 간 상태 어휘 통일 여부 (news-lab 저장소 작업)
```

---

# 확정된 지표 정의

| 지표 이름 | 타입 | label | 소스 | 공백 |
| --- | --- | --- | --- | --- |
| `newslab_articles_collected_total` | Counter | — | `sum(crawl_runs.inserted_count)` | 수집 |
| `newslab_articles_skipped_total` | Counter | — | `sum(crawl_runs.skipped_count)` | 수집 |
| `newslab_articles_stored` | Gauge | — | `count(articles)` | 16건 차이 |
| `newslab_article_embeddings_stored` | Gauge | — | `count(article_embeddings)` | ④ created 대체 |
| `newslab_pipeline_embedding_candidates_total` | Counter | `pipeline` | `sum(candidate_count)` | ③ |
| `newslab_pipeline_embedding_reused_total` | Counter | `pipeline` | `sum(embedding_count)` | ④ |
| `newslab_pipeline_embedding_missing_total` | Counter | `pipeline` | `sum(missing_embedding_count)` | ④ |
| `newslab_pipeline_runs_total` | Counter | `pipeline`, `status` | `count(*) group by status` | ①② |
| `newslab_pipeline_last_success_timestamp_seconds` | Gauge | `pipeline` | `max(finished_at) where success` | ② |
| `newslab_pipeline_topics_selected_total` | Counter | `pipeline` | `sum(selected_topic_count)` | ⑤ |
| `newslab_pipeline_topics_saved_total` | Counter | `pipeline` | `sum(saved_topic_count)` | ⑤ |
| `newslab_pipeline_topics_failed_total` | Counter | `pipeline` | `sum(failed_topic_count)` | ⑥ |
| `newslab_pipeline_items_processed_total` | Counter | `pipeline` | `sum(extraction_runs.success_count)` | ⑤ |
| `newslab_pipeline_items_failed_total` | Counter | `pipeline` | `sum(extraction_runs.failed_count)` | ⑥ |
| `newslab_exporter_scrape_error_total` | Counter | `query` | exporter 자체 | — |

**쿼리 6개**: crawl / articles+embeddings / pipeline-embedding / pipeline-runs / pipeline-topics / extraction
→ `errgroup`으로 동시 실행한다.

`pipeline` label 값: `rss_collector`, `extraction`, `three_day`, `weekly`
(daily는 run table 부재로 제외)

## 공통 규칙

- 조회 실패를 `0`으로 덮지 않는다. 해당 지표를 노출하지 않고
  `newslab_exporter_scrape_error_total{query}`를 올린다.
  (`news-lab` Grafana dashboard에서 `or vector(0)`을 거부한 판단과 같은 원칙)
- 누적값은 `prometheus.NewConstMetric(..., prometheus.CounterValue, ...)`로 노출한다.
  run table이 append-only임을 0-4에서 확인했다.
- label에 `article_id`, `topic_id`, URL 등 고유값을 넣지 않는다.
- 비율은 exporter가 계산하지 않는다. 원자값만 노출한다.
- `status` label 값을 하드코딩하지 않는다. DB group by 결과를 그대로 쓴다.

---

# 이번 조사에서 나온 발견

| # | 발견 | 성격 | 조치 |
| --- | --- | --- | --- |
| 1 | three_day 최근 2회(8/8, 8/9) `partial_success`. Job은 초록불 | 운영 | exporter가 노출하면 즉시 보임 |
| 2 | **extraction_runs 252회 전부 `success`인데 실패 47건.** 파이프라인마다 상태 판정 규칙이 다름 | 설계 결함 후보 | `items_failed_total` 별도 노출. 어휘 통일은 후속 |
| 3 | 임베딩 커버리지 73.3%의 82%가 6월 백필 부채. 정상 운영은 92%, 나머지 8%는 발행 지연으로 설계상 제외 | 구조 | 6월 백필 검토 (아래) |
| 4 | `articles` 9,629 vs `inserted_count` 9,613 = crawl_runs 도입 전 16건 | 계측 | 두 지표 모두 노출 |
| 5 | 임베딩 1건당 평균 3.85회 재사용, 중복 0건 실증 | 검증됨 | 이력서 근거 |
| 6 | 원문 추출 657/9,629 = 6.8% ≈ 이력서 7.4% | 검증됨 | 이력서 근거 |
| 7 | run table 3종 append-only → Counter 안전 | 확정 | — |
| 8 | 이력서 수집 수치가 낡음 (6,300 → 9,613) | — | **이력서 갱신 필요** |

## 6월 임베딩 백필 검토

2,112건 × 약 100 토큰 ≈ 211K 토큰 ≈ **$0.004**. 비용은 사실상 0이다.
백필하면 6월 기사가 토픽·검색 대상에 들어온다.
로드맵 Week 2(하이브리드 검색) 착수 전에 판단한다. **이번 exporter 범위는 아니다.**

## 검색 설계에 주는 제약 (로드맵 Week 2)

FTS는 9,629건 전체를 덮지만 벡터 검색은 7,061건(73.3%)만 덮는다.
RRF로 두 랭킹을 병합할 때 **커버리지가 다르다는 점을 전제로 설계해야 한다.**

---

# 미결정 / 후속 과제

| 항목 | 내용 | 왜 이번 범위가 아닌가 |
| --- | --- | --- |
| **LLM 토큰·비용** | `topics`, `three_day_topics`, `weekly_topics` 어디에도 토큰·비용 컬럼이 없다. "누적 $0.5 미만"의 근거는 **벤더 콘솔뿐**이다 | migration + pipeline 코드 양쪽 변경 필요. 비용 **통제** 메커니즘은 `unique (summary_input_hash, provider, model)`로 이미 스키마에 있다. 없는 것은 **소비량 측정** |
| **daily 실행 이력 table** | 0-1/0-2에서 부재 확정 | migration 추가는 `news-lab` 저장소 작업 |
| **파이프라인 간 상태 어휘 통일** | extraction은 부분 실패를 `success`로 처리 (발견 2) | pipeline 코드 변경 필요 |
| **⑦ stage별 duration** | run table에 stage 단위 시각이 없다 | pipeline 계측 추가 필요 |
| **⑧ Summary provider 오류 수** | provider API 오류가 별도 기록되지 않는다 | pipeline 오류 분류 추가 필요 |
| **Prometheus retention** | 현재 `1d`, PVC 없음. 장기 추세 불가 | `news-lab/k8s/monitoring` 작업 |
| **extraction_runs id 연속성** | 0-4에서 미확인 | 확인 후 Counter 안전성 확정 |
| **read-only DB 계정** | exporter용 계정 미발급 | 사람이 Supabase에서 수행 |

---

# 다음 단계

- [ ] `docs/tasks/`에 exporter 구현 task 작성
- [ ] `internal/collector/queries.go`에 위 6개 쿼리 반영
- [ ] `prometheus.Collector` 구현 (`Describe` / `Collect`)
- [ ] `context` timeout + `errgroup` 동시 실행
- [ ] read-only DB 계정 발급 (사람)
- [ ] Dockerfile multi-stage → 이미지 크기 측정 후 기록
- [ ] `k8s/` Deployment·Service·ServiceMonitor
      ⚠️ kube-prometheus-stack은 기본적으로 자기 release label이 붙은 ServiceMonitor만
      수집한다. `serviceMonitorSelectorNilUsesHelmValues` 확인 필요
- [ ] Grafana 패널 추가 (`news-lab/k8s/monitoring/dashboards/`)
