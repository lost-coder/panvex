package server

import (
	"context"
	"crypto/tls"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

const (
	certificateAuthorityLifetime = 5 * 365 * 24 * time.Hour
	serverCertificateLifetime    = 365 * 24 * time.Hour
	agentCertificateLifetime     = 30 * 24 * time.Hour
)

type issuedCertificate struct {
	CertificatePEM string
	PrivateKeyPEM  string
	CAPEM          string
	ExpiresAt      time.Time
}

type certificateAuthority struct {
	certificate       *x509.Certificate
	privateKey        *ecdsa.PrivateKey
	caPEM             string
	serverCertificate tls.Certificate
}

func newCertificateAuthority(now time.Time) (*certificateAuthority, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Panvex Control Plane Root CA",
			Organization: []string{"Panvex"},
		},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certificateAuthorityLifetime),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, privateKey.Public(), privateKey)
	if err != nil {
		return nil, err
	}

	parsedCertificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}

	return buildCertificateAuthority(parsedCertificate, privateKey, encodePEM("CERTIFICATE", der), now)
}

func loadOrCreateCertificateAuthority(store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	if store == nil {
		return newCertificateAuthority(now)
	}

	record, err := store.GetCertificateAuthority(context.Background())
	if err == nil {
		if encryptionKey != "" {
			decrypted, decErr := decryptPEM(record.PrivateKeyPEM, encryptionKey)
			if decErr != nil {
				return nil, decErr
			}
			record.PrivateKeyPEM = decrypted
		}
		authority, err := certificateAuthorityFromRecord(record, now)
		if err != nil {
			return nil, err
		}
		remaining := authority.certificate.NotAfter.Sub(now)
		if remaining <= 0 {
			slog.Warn("control-plane CA certificate expired, regenerating", "expired_ago", (-remaining).String())
			return regenerateCertificateAuthority(store, now, encryptionKey)
		}
		if remaining < 30*24*time.Hour {
			slog.Warn("control-plane CA certificate expiring soon", "remaining", remaining.Round(time.Hour).String())
		}
		// Re-encrypt if the stored key was plaintext but an encryption key is now configured.
		if encryptionKey != "" && !isEncryptedPEM(record.PrivateKeyPEM) {
			rec, recErr := authority.record(now, encryptionKey)
			if recErr == nil {
				_ = store.PutCertificateAuthority(context.Background(), rec)
			}
		}
		return authority, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}

	authority, err := newCertificateAuthority(now)
	if err != nil {
		return nil, err
	}

	record, err = authority.record(now, encryptionKey)
	if err != nil {
		return nil, err
	}
	if err := store.PutCertificateAuthority(context.Background(), record); err != nil {
		return nil, err
	}

	return authority, nil
}

func regenerateCertificateAuthority(store storage.CertificateAuthorityStore, now time.Time, encryptionKey string) (*certificateAuthority, error) {
	authority, err := newCertificateAuthority(now)
	if err != nil {
		return nil, err
	}
	record, err := authority.record(now, encryptionKey)
	if err != nil {
		return nil, err
	}
	if err := store.PutCertificateAuthority(context.Background(), record); err != nil {
		return nil, err
	}
	return authority, nil
}

func certificateAuthorityFromRecord(record storage.CertificateAuthorityRecord, now time.Time) (*certificateAuthority, error) {
	certificateBlock, _ := pem.Decode([]byte(record.CAPEM))
	if certificateBlock == nil {
		return nil, errors.New("failed to decode persisted control-plane CA certificate")
	}

	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return nil, err
	}

	privateKeyBlock, _ := pem.Decode([]byte(record.PrivateKeyPEM))
	if privateKeyBlock == nil {
		return nil, errors.New("failed to decode persisted control-plane CA private key")
	}

	privateKey, err := parseAuthorityPrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return nil, err
	}

	return buildCertificateAuthority(certificate, privateKey, record.CAPEM, now)
}

func parseAuthorityPrivateKey(encoded []byte) (*ecdsa.PrivateKey, error) {
	privateKey, err := x509.ParseECPrivateKey(encoded)
	if err == nil {
		return privateKey, nil
	}

	parsedKey, pkcs8Err := x509.ParsePKCS8PrivateKey(encoded)
	if pkcs8Err != nil {
		return nil, err
	}

	ecdsaKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("persisted control-plane CA private key must be ECDSA")
	}

	return ecdsaKey, nil
}

func buildCertificateAuthority(certificate *x509.Certificate, privateKey *ecdsa.PrivateKey, caPEM string, now time.Time) (*certificateAuthority, error) {
	serverPair, err := issueServerCertificate(certificate, privateKey, now)
	if err != nil {
		return nil, err
	}

	return &certificateAuthority{
		certificate:       certificate,
		privateKey:        privateKey,
		caPEM:             caPEM,
		serverCertificate: serverPair,
	}, nil
}

func (a *certificateAuthority) record(now time.Time, encryptionKey string) (storage.CertificateAuthorityRecord, error) {
	privateDER, err := x509.MarshalECPrivateKey(a.privateKey)
	if err != nil {
		return storage.CertificateAuthorityRecord{}, err
	}

	keyPEM := encodePEM("EC PRIVATE KEY", privateDER)
	if encryptionKey != "" {
		encrypted, encErr := encryptPEM(keyPEM, encryptionKey)
		if encErr != nil {
			return storage.CertificateAuthorityRecord{}, encErr
		}
		keyPEM = encrypted
	}

	return storage.CertificateAuthorityRecord{
		CAPEM:         a.caPEM,
		PrivateKeyPEM: keyPEM,
		UpdatedAt:     now.UTC(),
	}, nil
}

func (a *certificateAuthority) issueClientCertificate(commonName string, now time.Time) (issuedCertificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return issuedCertificate{}, err
	}

	serial, err := randomSerial()
	if err != nil {
		return issuedCertificate{}, err
	}

	expiresAt := now.Add(agentCertificateLifetime)
	certificate := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Panvex Agents"},
		},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		SubjectKeyId: serial.Bytes(),
	}

	der, err := x509.CreateCertificate(rand.Reader, certificate, a.certificate, privateKey.Public(), a.privateKey)
	if err != nil {
		return issuedCertificate{}, err
	}

	privateDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return issuedCertificate{}, err
	}

	return issuedCertificate{
		CertificatePEM: encodePEM("CERTIFICATE", der),
		PrivateKeyPEM:  encodePEM("EC PRIVATE KEY", privateDER),
		CAPEM:          a.caPEM,
		ExpiresAt:      expiresAt,
	}, nil
}

func encodePEM(blockType string, bytes []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  blockType,
		Bytes: bytes,
	}))
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func (a *certificateAuthority) serverTLSConfig() *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM([]byte(a.caPEM))

	return &tls.Config{
		Certificates: []tls.Certificate{a.serverCertificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
}

func issueServerCertificate(caCertificate *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time) (tls.Certificate, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "control-plane.panvex.internal",
			Organization: []string{"Panvex"},
		},
		DNSNames:     []string{"localhost", "control-plane.panvex.internal"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(serverCertificateLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCertificate, privateKey.Public(), caKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	privateDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.X509KeyPair(
		[]byte(encodePEM("CERTIFICATE", der)),
		[]byte(encodePEM("EC PRIVATE KEY", privateDER)),
	)
}
