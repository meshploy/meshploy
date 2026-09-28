# Short names, as most Dockerfiles write them: Buildah must read them as
# Docker Hub. Two stages, a build argument, and a RUN that downloads.
FROM golang:1.24-alpine AS build
ARG GREETING=unset
WORKDIR /src
COPY main.go .
RUN go mod init smoke && CGO_ENABLED=0 go build -ldflags "-X main.greeting=${GREETING}" -o /smoke .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /smoke /smoke
CMD ["/smoke"]
