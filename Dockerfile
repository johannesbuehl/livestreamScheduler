FROM golang:1.25.1-alpine AS build
WORKDIR /tmp/app

# copy everything over
COPY . .

# install dependencies
RUN go mod download

RUN go build -o app


FROM alpine:latest

WORKDIR /usr/local/app

COPY --from=build /tmp/app/app .

CMD ["./app"]