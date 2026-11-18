package app

import (
	"crypto/tls"

	"github.com/icloudza/ResidentKit/server/internal/config"
)

// gatewayTLS 网关自己做 TLS 时的证书（gateway.tls_cert / tls_key）；前面有反代负责 HTTPS 时返回 nil。
func gatewayTLS(cfg *config.Config) (*tls.Config, tls.Certificate, error) {
	if !cfg.GatewayTLS() {
		return nil, tls.Certificate{}, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.Gateway.TLSCert, cfg.Gateway.TLSKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, cert, nil
}
