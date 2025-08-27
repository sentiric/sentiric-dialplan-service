package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"os"

	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func NewServer(tlsConfig config.Config) (*grpc.Server, error) {
	certificate, err := tls.LoadX509KeyPair(tlsConfig.TLS.CertPath, tlsConfig.TLS.KeyPath)
	if err != nil {
		return nil, err
	}
	caCert, err := os.ReadFile(tlsConfig.TLS.CAPath)
	if err != nil {
		return nil, err
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, err
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	return grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg))), nil
}
