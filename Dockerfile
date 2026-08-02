################################################################################
FROM golang:1.26 AS build

WORKDIR /opt/build

COPY go.mod go.sum ./
RUN go mod download

ARG git_sha
ARG git_ts


COPY --parents Makefile *.go internal ./
RUN mkdir -p /opt/app \
  && make clean \
  && make bin/whoami GIT_SHA=${git_sha} GIT_TS=${git_ts} \
  && mv bin/whoami /opt/app/whoami

COPY --parents static templates /opt/app/
RUN echo ${git_ts}-${git_sha} > /opt/app/VERSION

################################################################################
FROM gcr.io/distroless/base-debian13 AS release

WORKDIR /opt/app

COPY --from=build /opt/app ./

ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot

CMD [ "/opt/app/whoami" ]
