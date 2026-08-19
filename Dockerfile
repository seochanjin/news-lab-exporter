# --- build stage -----------------------------------------------------------
FROM golang:1.26-alpine AS build

WORKDIR /src

# 의존성 목록만 먼저 복사한다. 소스가 바뀌어도 go.mod/go.sum이 그대로면
# 이 레이어가 캐시되어 재빌드가 빨라진다.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 : libc에 의존하지 않는 정적 바이너리를 만든다. distroless에 필요하다.
# -trimpath     : 빌드 머신의 절대 경로를 바이너리에서 제거한다.
# -ldflags -s -w: 디버그 심볼과 DWARF 정보를 빼 크기를 줄인다.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/news-lab-exporter .

# --- runtime stage ---------------------------------------------------------
# distroless static은 셸도 패키지 매니저도 없다. 공격 표면이 작고 이미지가 가볍다.
# nonroot 태그는 uid 65532로 실행된다.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/news-lab-exporter /news-lab-exporter

EXPOSE 9310

USER nonroot:nonroot

ENTRYPOINT ["/news-lab-exporter"]
