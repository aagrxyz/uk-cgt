FROM golang:alpine

RUN apk add --no-cache curl tzdata

WORKDIR /finances/src/
COPY ./src ./
RUN go mod download
RUN CGO_ENABLED=0 go build -o /finances/finances

WORKDIR /finances/
COPY ./static ./

EXPOSE 7777/tcp

CMD "/finances/finances"
