module github.com/sentiric/sentiric-dialplan-service

go 1.23.0

toolchain go1.24.5

require (
	// YENİ: Merkezi kontratlarımızı bir bağımlılık olarak ekliyoruz.
	github.com/sentiric/sentiric-contracts v0.4.2
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2 // indirect
)

require github.com/jackc/pgx/v5 v5.7.5

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.24.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
)
