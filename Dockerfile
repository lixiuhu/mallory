FROM golang:latest
COPY . /go/src/mallory
WORKDIR /go/src/mallory/cmd/mallory
RUN go get .
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' .
RUN CGO_ENABLED=0 GOOS=linux go install -a -ldflags '-extldflags "-static"'
ENTRYPOINT ["/go/bin/mallory"]
