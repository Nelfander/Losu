# Step 1: Build Stage
FROM golang:alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary specifically from the logsum directory
RUN CGO_ENABLED=0 GOOS=linux go build -o /losu cmd/logsum/main.go

# Build the generators
RUN CGO_ENABLED=0 GOOS=linux go build -o /normal_gen bin/normal/normal_gen.go
RUN CGO_ENABLED=0 GOOS=linux go build -o /stress_gen bin/stress/stress_gen.go

# Step 2: Final Stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates


WORKDIR /app

# Copy all binaries into /app
COPY --from=builder /losu .
COPY --from=builder /normal_gen .
COPY --from=builder /stress_gen .

# Copy the .env file so godotenv can find it!
COPY .env .

# Run the app
ENTRYPOINT ["./losu"]