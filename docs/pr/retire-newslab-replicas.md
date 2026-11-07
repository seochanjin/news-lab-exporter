# chore: NewsLab exporter 종료 상태를 replicas 0으로 반영

운영 종료한 exporter의 Deployment replicas를 1에서 0으로 변경한다. 배포 설정을 다시 적용해도 Pod가 시작되지 않으며, 재개 시 1로 변경하도록 주석을 남긴다.

검증: YAML 파싱·구조 비교, `git diff --check`, `go build ./...`, `go vet ./...`, `go test ./...`, `gofmt -l .` 통과. 실제 결과는 `docs/verification/retire-newslab-replicas.md`에 기록했다.

이번 PR은 저장소 설정과 작업 기록만 변경한다. 운영 서버 적용과 병합은 수행하지 않는다.
