package containerstore

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"time"

	multierror "github.com/hashicorp/go-multierror"
	uuid "github.com/nu7hatch/gouuid"
	"github.com/tedsuo/ifrit"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/routing-info/internalroutes"
)

const (
	CredCreationSucceededCount    = "CredCreationSucceededCount"
	CredCreationSucceededDuration = "CredCreationSucceededDuration"
	CredCreationFailedCount       = "CredCreationFailedCount"
)

type Credentials struct {
	InstanceIdentityCredential Credential
	C2CCredential              Credential
}

type Credential struct {
	Cert string
	Key  string
}

//go:generate counterfeiter -o containerstorefakes/fake_cred_manager.go . CredManager
type CredManager interface {
	CreateCredDir(lager.Logger, executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error)
	RemoveCredDir(lager.Logger, executor.Container) error
	Runner(lager.Logger, executor.Container) ifrit.Runner
}

type noopManager struct{}

func NewNoopCredManager() CredManager {
	return &noopManager{}
}

func (c *noopManager) CreateCredDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	return nil, nil, nil
}

func (c *noopManager) RemoveCredDir(logger lager.Logger, container executor.Container) error {
	return nil
}

func (c *noopManager) Runner(lager.Logger, executor.Container) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		<-signals
		return nil
	})
}

type credManager struct {
	logger         lager.Logger
	metronClient   loggingclient.IngressClient
	validityPeriod time.Duration
	entropyReader  io.Reader
	clock          clock.Clock
	CaCert         *x509.Certificate
	privateKey     *rsa.PrivateKey
	handlers       []CredentialHandler
}

//go:generate counterfeiter -o containerstorefakes/fake_cred_handler.go . CredentialHandler

// CredentialHandler handles new credential generated by the CredManager.
type CredentialHandler interface {
	// Called to create the necessary directory
	CreateDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error)

	// Called during shutdown to remove directory created in CreateDir
	RemoveDir(logger lager.Logger, container executor.Container) error

	// Called periodically as new valid certificate/key pair are generated
	Update(credentials Credentials, container executor.Container) error

	// Called when the CredManager is preparing to exit. This is mainly to update
	// the EnvoyProxy with invalid certificates and prevent it from accepting
	// more incoming traffic from the gorouter
	Close(invalidCredentials Credentials, container executor.Container) error
}

func NewCredManager(
	logger lager.Logger,
	metronClient loggingclient.IngressClient,
	validityPeriod time.Duration,
	entropyReader io.Reader,
	clock clock.Clock,
	CaCert *x509.Certificate,
	privateKey *rsa.PrivateKey,
	handlers ...CredentialHandler,
) CredManager {
	return &credManager{
		logger:         logger,
		metronClient:   metronClient,
		validityPeriod: validityPeriod,
		entropyReader:  entropyReader,
		clock:          clock,
		CaCert:         CaCert,
		privateKey:     privateKey,
		handlers:       handlers,
	}
}

func calculateCredentialRotationPeriod(validityPeriod time.Duration) time.Duration {
	if validityPeriod > 4*time.Hour {
		return validityPeriod - 30*time.Minute
	}

	eighth := validityPeriod / 8
	return validityPeriod - eighth
}

func (c *credManager) CreateCredDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	var mounts []garden.BindMount
	var envs []executor.EnvironmentVariable
	for _, h := range c.handlers {
		handlerMounts, handlerEnv, err := h.CreateDir(logger, container)
		if err != nil {
			return nil, nil, err
		}
		envs = append(envs, handlerEnv...)
		mounts = append(mounts, handlerMounts...)
	}
	return mounts, envs, nil
}

func (c *credManager) RemoveCredDir(logger lager.Logger, container executor.Container) error {
	err := &multierror.Error{ErrorFormat: func(errs []error) string {
		var s string
		for _, e := range errs {
			if e != nil {
				if s != "" {
					s += "; "
				}
				s += e.Error()
			}
		}
		return s
	}}

	for _, h := range c.handlers {
		handlerErr := h.RemoveDir(logger, container)
		if handlerErr != nil {
			err = multierror.Append(err, handlerErr)
		}
	}
	return err.ErrorOrNil()
}

func (c *credManager) Runner(logger lager.Logger, node *storeNode) ifrit.Runner {
	runner := ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		logger = logger.Session("cred-manager-runner")
		logger.Info("starting")
		defer logger.Info("complete")

		initialContainer := node.Info()
		start := c.clock.Now()
		creds, err := c.generateCreds(logger, initialContainer, initialContainer.Guid)
		if err != nil {
			logger.Error("failed-to-generate-credentials", err)
			c.metronClient.IncrementCounter(CredCreationFailedCount)
			return err
		}

		duration := c.clock.Since(start)

		for _, h := range c.handlers {
			err := h.Update(creds, initialContainer)
			if err != nil {
				return err
			}
		}

		c.metronClient.IncrementCounter(CredCreationSucceededCount)
		c.metronClient.SendDuration(CredCreationSucceededDuration, duration)

		rotationDuration := calculateCredentialRotationPeriod(c.validityPeriod)
		regenCertTimer := c.clock.NewTimer(rotationDuration)

		close(ready)

		regenLogger := logger.Session("regenerating-cert-and-key")
		for {
			select {
			case <-regenCertTimer.C():
				regenLogger.Debug("started")
				container := node.Info()
				start := c.clock.Now()
				creds, err := c.generateCreds(logger, container, container.Guid)
				duration := c.clock.Since(start)
				if err != nil {
					regenLogger.Error("failed-to-generate-credentials", err)
					c.metronClient.IncrementCounter(CredCreationFailedCount)
					return err
				}
				c.metronClient.IncrementCounter(CredCreationSucceededCount)
				c.metronClient.SendDuration(CredCreationSucceededDuration, duration)

				for _, h := range c.handlers {
					err := h.Update(creds, container)
					if err != nil {
						return err
					}
				}

				rotationDuration = calculateCredentialRotationPeriod(c.validityPeriod)
				regenCertTimer.Reset(rotationDuration)
				regenLogger.Debug("completed")
			case signal := <-signals:
				container := node.Info()
				logger.Info("signalled", lager.Data{"signal": signal.String()})
				cred, err := c.generateCreds(logger, container, "")
				if err != nil {
					regenLogger.Error("failed-to-generate-credentials", err)
					c.metronClient.IncrementCounter(CredCreationFailedCount)
					return err
				}
				for _, h := range c.handlers {
					h.Close(cred, container)
				}
				return nil
			}
		}
	})

	return runner
}

const (
	certificatePEMBlockType = "CERTIFICATE"
	privateKeyPEMBlockType  = "RSA PRIVATE KEY"
)

func (c *credManager) generateCreds(logger lager.Logger, container executor.Container, certGUID string) (Credentials, error) {
	logger = logger.Session("generating-credentials")
	logger.Debug("starting")
	defer logger.Debug("complete")

	ipForCert := container.InternalIP
	if len(ipForCert) == 0 {
		ipForCert = container.ExternalIP
	}

	logger.Debug("generating-credentials-for-instance-identity")
	idCred, err := c.generateCredForSAN(logger,
		certificateSAN{IPAddress: ipForCert, OrganizationalUnits: container.CertificateProperties.OrganizationalUnit},
		certGUID,
	)
	if err != nil {
		return Credentials{}, err
	}

	logger.Debug("generating-credentials-for-c2c")
	c2cCred, err := c.generateCredForSAN(logger,
		certificateSAN{InternalRoutes: container.InternalRoutes, OrganizationalUnits: container.CertificateProperties.OrganizationalUnit},
		certGUID,
	)
	if err != nil {
		return Credentials{}, err
	}

	return Credentials{
		InstanceIdentityCredential: idCred,
		C2CCredential:              c2cCred,
	}, nil
}

func (c *credManager) generateCredForSAN(logger lager.Logger, certSAN certificateSAN, certGUID string) (Credential, error) {
	logger.Debug("generating-private-key")
	privateKey, err := rsa.GenerateKey(c.entropyReader, 2048)
	if err != nil {
		return Credential{}, err
	}
	logger.Debug("generated-private-key")

	startValidity := c.clock.Now()

	template := createCertificateTemplate(certGUID,
		certSAN,
		startValidity,
		startValidity.Add(c.validityPeriod),
	)

	logger.Debug("generating-serial-number")
	guid, err := uuid.NewV4()
	if err != nil {
		logger.Error("failed-to-generate-uuid", err)
		return Credential{}, err
	}
	logger.Debug("generated-serial-number")

	guidBytes := [16]byte(*guid)
	template.SerialNumber.SetBytes(guidBytes[:])

	logger.Debug("generating-certificate")
	certBytes, err := x509.CreateCertificate(c.entropyReader, template, c.CaCert, privateKey.Public(), c.privateKey)
	if err != nil {
		return Credential{}, err
	}
	logger.Debug("generated-certificate")

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	var keyBuf bytes.Buffer
	err = pemEncode(privateKeyBytes, privateKeyPEMBlockType, &keyBuf)
	if err != nil {
		return Credential{}, err
	}

	var certificateBuf bytes.Buffer
	certificateWriter := &certificateBuf
	err = pemEncode(certBytes, certificatePEMBlockType, certificateWriter)
	if err != nil {
		return Credential{}, err
	}

	err = pemEncode(c.CaCert.Raw, certificatePEMBlockType, certificateWriter)
	if err != nil {
		return Credential{}, err
	}

	return Credential{
		Cert: certificateBuf.String(),
		Key:  keyBuf.String(),
	}, nil
}

func pemEncode(bytes []byte, blockType string, writer io.Writer) error {
	block := &pem.Block{
		Type:  blockType,
		Bytes: bytes,
	}
	return pem.Encode(writer, block)
}

type certificateSAN struct {
	IPAddress           string
	InternalRoutes      internalroutes.InternalRoutes
	OrganizationalUnits []string
}

func createCertificateTemplate(guid string, certSAN certificateSAN, notBefore, notAfter time.Time) *x509.Certificate {
	var ipaddr []net.IP
	if len(certSAN.IPAddress) == 0 {
		ipaddr = []net.IP{}
	} else {
		ipaddr = []net.IP{net.ParseIP(certSAN.IPAddress)}
	}
	dnsNames := []string{guid}
	for _, route := range certSAN.InternalRoutes {
		dnsNames = append(dnsNames, route.Hostname)
	}

	return &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:         guid,
			OrganizationalUnit: certSAN.OrganizationalUnits,
		},
		IPAddresses: ipaddr,
		DNSNames:    dnsNames,
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
}
