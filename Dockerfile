FROM golang:alpine

RUN apk add --no-cache curl tzdata

WORKDIR /finances/src/
COPY ./src ./
RUN go mod download
RUN go build -o /finances/finances

WORKDIR /finances/

EXPOSE 7777/tcp

CMD "/finances/finances"
