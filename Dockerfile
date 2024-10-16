# Build Stage
FROM golang:1.23-bullseye AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o main .

# Run Stage
FROM mcr.microsoft.com/dotnet/sdk:6.0

WORKDIR /root/

# Copy the binary from the build stage
COPY --from=builder /app/main .
COPY index.html .

# Command to run the executable
CMD ["./main"]