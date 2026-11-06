FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /fleetd ./cmd/fleetd

FROM alpine:3.20
RUN adduser -D fleet
COPY --from=build /fleetd /usr/local/bin/fleetd
USER fleet
EXPOSE 8090
ENTRYPOINT ["fleetd"]
