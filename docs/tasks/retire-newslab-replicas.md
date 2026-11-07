# Task: NewsLab exporter 종료 상태 반영

Branch: `codex/retire-newslab-replicas`

## 목표와 범위

운영 종료한 exporter가 배포 설정 재적용만으로 다시 실행되지 않도록
`k8s/deployment.yaml`의 `spec.replicas`를 1에서 0으로 변경한다.
애플리케이션 코드, DB, Secret, 이미지와 서비스 설정은 변경하지 않는다.
서버 적용은 이번 작업에 포함하지 않는다. 초기에는 로컬 변경만 준비했으며,
2026-11-07 사용자의 후속 요청에 따라 커밋·푸시와 main 대상 PR 생성을 진행한다.

## Checklist

- [x] 기존 작업 상태와 배포 설정 확인: clean main, replicas 1, HPA 없음.
- [x] replicas 0과 의도·재개 조건을 설명하는 주석 반영.
- [x] YAML과 변경 범위 검증 및 최소 Go 검증 결과 기록.
- [x] PR 초안과 개발 기록 작성.
- [x] PR 발행 전 최신 origin/main과 동일한 시작점임을 확인: ahead 0, behind 0.

## Result

변경 파일:

- `k8s/deployment.yaml`
- `docs/tasks/retire-newslab-replicas.md`
- `docs/verification/retire-newslab-replicas.md`
- `docs/pr/retire-newslab-replicas.md`
- `docs/devlog/retire-newslab-replicas.md`

YAML 파싱·구조 비교, `git diff --check`, Go build/vet/test/gofmt 검증을 완료했다.
실제 실행 명령과 결과는 `docs/verification/retire-newslab-replicas.md`에 기록한다.
운영에 적용하지 않았으며 재개 시에는 사용자 확인 후 replicas를 1로 변경해야 한다.
