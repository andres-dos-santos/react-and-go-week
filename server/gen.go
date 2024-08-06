package gen

//go:generate go run ./cmd/tools/terndotenv/main.go
//go:generate go run sqlc generate -f ./internal/store/pgstore/sqlc.yaml
