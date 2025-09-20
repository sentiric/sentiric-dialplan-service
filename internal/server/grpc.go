// sentiric-dialplan-service/internal/server/grpc.go
package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// NewServer, TLS kimlik bilgilerini yükleyerek yeni bir gRPC sunucu örneği oluşturur.
func NewServer(cfg config.Config) (*grpc.Server, error) {
	certificate, err := tls.LoadX509KeyPair(cfg.TLS.CertPath, cfg.TLS.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("sunucu sertifikası yüklenemedi: %w", err)
	}

	caCert, err := os.ReadFile(cfg.TLS.CaPath)
	if err != nil {
		return nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
	}

	return grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsCfg))), nil
}