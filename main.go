package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"

	fmt "fmt"
	"log"
	"math/big"
	"net"
	"sync"
	"time"
)

type ChatConfig struct {
	wg      sync.WaitGroup
	ready   chan struct{}
	allDone chan struct{}
	handler func(ChatStream)
}

func (cfg *ChatConfig) Hello(stream Chat_HelloServer) error {
	cfg.handler(stream)
	return nil
}

type ChatStream interface {
	Send(*ChatMessage) error
	Recv() (*ChatMessage, error)
}

func peerTask(stream ChatStream) {
	// fmt.Println("peerTask")
	// defer fmt.Println("peerTask finishes")

	for j := 0; j < 10000; j++ {
		err := stream.Send(&ChatMessage{"Hello!"})
		if err != nil {
			log.Fatalf("peerTask:stream.Send: %+v", err)
		}
	}
}

func chaincodeTask(stream ChatStream) {
	// fmt.Println("chaincodeTask")
	// defer fmt.Println("chaincodeTask finishes")
	for {
		_, err := stream.Recv()
		if err != nil {
			return
		}
	}
}

type serialStream struct {
	cs   ChatStream
	lock sync.Mutex
}

func (stream *serialStream) Send(msg *ChatMessage) error {
	stream.lock.Lock()
	defer stream.lock.Unlock()
	return stream.cs.Send(msg)
}

func (stream *serialStream) Recv() (*ChatMessage, error) {
	stream.lock.Lock()
	defer stream.lock.Unlock()
	return stream.cs.Recv()
}

func generate() (*tls.Certificate, *x509.CertPool, *tls.Certificate, *x509.CertPool) {

	template := x509.Certificate{
		SerialNumber:          big.NewInt(12345),
		Subject:               pkix.Name{Organization: []string{"localhost"}},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate private key: %s", err)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %s", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Fatalf("Unable to marshal ECDSA private key: %v", err)
	}

	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	keyPair, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		log.Fatalf("tls.X509KeyPair: %v", err)
	}

	rootCA := x509.NewCertPool()
	if ok := rootCA.AppendCertsFromPEM(pemCert); !ok {
		log.Fatalf("Failed to load Root CA")
	}

	templateC := x509.Certificate{
		SerialNumber:          big.NewInt(54321),
		Subject:               pkix.Name{Organization: []string{"localhost"}},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
		IsCA:                  false,
	}

	keyC, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("failed to generate private key: %s", err)
	}

	derBytesC, err := x509.CreateCertificate(rand.Reader, &templateC, &template, &keyC.PublicKey, key)
	if err != nil {
		log.Fatalf("Failed to create certificate: %s", err)
	}

	keyBytesC, err := x509.MarshalECPrivateKey(keyC)
	if err != nil {
		log.Fatalf("Unable to marshal ECDSA private key: %v", err)
	}

	pemCertC := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytesC})
	pemKeyC := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytesC})

	keyPairC, err := tls.X509KeyPair(pemCertC, pemKeyC)
	if err != nil {
		log.Fatalf("tls.X509KeyPair: %v", err)
	}

	clientCertPool := x509.NewCertPool()
	clientCertPool.AppendCertsFromPEM(pemCertC)

	return &keyPair, rootCA, &keyPairC, clientCertPool
}

func initialize(cfg *ChatConfig) {
	cfg.wg = sync.WaitGroup{}
	cfg.ready = make(chan struct{}, 0)
	cfg.allDone = make(chan struct{}, 0)
}

func measure(label string, task func()) {
	start := time.Now()
	task()
	end := time.Now()
	fmt.Printf("%-10s Elapsed %f\n", label+":", (end.Sub(start)).Seconds())
}

func main() {
	port := 8888
	keyPair, rootCA, clientKeyPair, clientCert := generate()

	normal := &Normal{port: port}
	initialize(&normal.ChatConfig)

	measure("Normal", func() {
		go normal.peer(keyPair)
		normal.chaincode(rootCA)
	})

	time.Sleep(time.Second)

	sharding := &Sharding{port: port, poolSize: 32}
	initialize(&sharding.ChatConfig)

	measure("Sharding", func() {
		go sharding.peer(keyPair)
		sharding.chaincode(rootCA)
	})

	time.Sleep(time.Second)

	Opposite := Opposite{port: port}
	initialize(&Opposite.ChatConfig)

	measure("Opposite", func() {
		go Opposite.peer(rootCA)
		Opposite.chaincode(keyPair)
	})

	time.Sleep(time.Second)

	Reversal := &Reversal{port: port}
	initialize(&Reversal.ChatConfig)

	measure("Reversal", func() {
		go Reversal.peer(keyPair, rootCA)
		Reversal.chaincode(rootCA, clientKeyPair, clientCert)
	})

}
