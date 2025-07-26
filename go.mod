module github.com/sentiric/sentiric-dialplan-service

go 1.22.5

require (
	// YENİ: Merkezi kontratlarımızı bir bağımlılık olarak ekliyoruz.
	github.com/sentiric/sentiric-contracts v0.2.0
	go.uber.org/zap v1.27.0
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.2 // indirect
)

require (
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.27.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	golang.org/x/text v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240701130421-f6361c86f094 // indirect
)
