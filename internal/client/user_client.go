// sentiric-dialplan-service/internal/client/user_client.go
package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	userv1 "github.com/sentiric/sentiric-contracts/gen/go/sentiric/user/v1"
	"github.com/sentiric/sentiric-dialplan-service/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// NewUserServiceClient, mTLS ile güvenli bir şekilde user-service'e bağlanır.
func NewUserServiceClient(targetURL string, cfg config.Config) (userv1.UserServiceClient, *grpc.ClientConn, error) {
	clientCert, err := tls.LoadX509KeyPair(cfg.TLS.CertPath, cfg.TLS.KeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("istemci sertifikası yüklenemedi: %w", err)
	}
	caCert, err := os.ReadFile(cfg.TLS.CaPath)
	if err != nil {
		return nil, nil, fmt.Errorf("CA sertifikası okunamadı: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, nil, fmt.Errorf("CA sertifikası havuza eklenemedi")
	}

	cleanTarget := targetURL
	if strings.Contains(targetURL, "://") {
		parts := strings.Split(targetURL, "://")
		if len(parts) > 1 {
			cleanTarget = parts[1]
		}
	}

	serverName := strings.Split(cleanTarget, ":")[0]

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caCertPool,
		ServerName:   serverName,
	})

	conn, err := grpc.NewClient(cleanTarget, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("user-service'e bağlanılamadı: %w", err)
	}
	return userv1.NewUserServiceClient(conn), conn, nil
}
