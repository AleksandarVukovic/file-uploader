package tls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

func validateFiles(certFile, keyFile, caFile string) error {
	if certFile == "" || keyFile == "" || caFile == "" {
		return fmt.Errorf("tls: tlsCertFile, tlsKeyFile, and tlsCACertFile must be provided for mTLS")
	}
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		return fmt.Errorf("tls: tlsCertFile %q does not exist", certFile)
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return fmt.Errorf("tls: tlsKeyFile %q does not exist", keyFile)
	}
	if _, err := os.Stat(caFile); os.IsNotExist(err) {
		return fmt.Errorf("tls: tlsCACertFile %q does not exist", caFile)
	}
	return nil
}

func loadCAPool(caFile string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("tls: read CA cert %q: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("tls: no valid certificates found in %q", caFile)
	}
	return pool, nil
}

func NewServerConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	if err := validateFiles(certFile, keyFile, caFile); err != nil {
		return nil, err
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: load server key pair: %w", err)
	}

	pool, err := loadCAPool(caFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func NewClientConfig(certFile, keyFile, caFile string) (*tls.Config, error) {
	if err := validateFiles(certFile, keyFile, caFile); err != nil {
		return nil, err
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: load client key pair: %w", err)
	}

	pool, err := loadCAPool(caFile)
	if err != nil {
		return nil, err
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}
