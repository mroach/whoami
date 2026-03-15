FROM golang:1.25 AS build

WORKDIR /opt/build

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /opt/app/whoami
COPY --parents static pages /opt/app/

FROM gcr.io/distroless/base-debian13 AS release

WORKDIR /opt/app

COPY --from=build /opt/app ./

ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot

CMD [ "/opt/app/whoami" ]
