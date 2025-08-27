package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func NewClientConnection(targetURL, serverName string, tlsConfig config.Config) (*grpc.ClientConn, error) {
	clientCert, err := tls.LoadX509KeyPair(tlsConfig.TLS.CertPath, tlsConfig.TLS.KeyPath)
	if err != nil {
		return nil, err
	}
	caCert, err := os.ReadFile(tlsConfig.TLS.CAPath)
	if err != nil {
		return nil, err
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, err
	}

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
	})

	return grpc.NewClient(targetURL, grpc.WithTransportCredentials(creds))
}
