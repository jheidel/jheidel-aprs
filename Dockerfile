####
# Build the go binary
####

FROM golang:alpine AS builder-go
RUN apk add --no-cache git

WORKDIR /go/src/jheidel-aprs/

# Copy all source files.
COPY . .

# Build the standalone executable.
RUN go get ./...
RUN go build

####
# Compose everything into a final minimal image.
####

FROM alpine
WORKDIR /app
COPY --from=builder-go /go/src/jheidel-aprs/jheidel-aprs /app

# Use local timezone.
# TODO use system time instead of hardcoded.
RUN apk add --update tzdata
ENV TZ=America/Los_Angeles

CMD ["./jheidel-aprs"]
