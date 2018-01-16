package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	fmt "fmt"
	"log"
	"net"
	"sync"
	"time"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type Reversal struct {
	ChatConfig
	port int
}

func (cfg *Reversal) peer(keyPair *tls.Certificate, rootCA *x509.CertPool) {
	config := tls.Config{
		Certificates: []tls.Certificate{*keyPair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootCA,
	}

	config.BuildNameToCertificate()

	lis, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.port), &config)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	for {
		tlsConn, err := lis.Accept()
		if err != nil {
			break
		}

		err = tlsConn.(*tls.Conn).Handshake()
		if err != nil {
			log.Fatalf("Handshake failed: %v", err)
		}

		//fmt.Printf("tlsConn=%# v", pretty.Formatter(tlsConn))

		info := credentials.TLSInfo{State: tlsConn.(*tls.Conn).ConnectionState()}
		ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: info})
		ctx = context.WithValue(ctx, "info", info)

		go func() {
			dialer := func(_ string, _ time.Duration) (net.Conn, error) {
				return tlsConn, nil
			}

			conn, err := grpc.Dial("dummy", grpc.WithInsecure(), grpc.WithDialer(dialer))
			if err != nil {
				log.Fatalf("grpc.Dial: %v", err)
			}

			cfg.wg = sync.WaitGroup{}
			for j := 0; j < 128; j++ {
				cfg.wg.Add(1)

				go func() {
					client := NewChatClient(conn)
					stream, err := client.Hello(ctx)
					if err != nil {
						log.Fatalf("%v.Hello(_) = _, %v", client, err)
					}
					defer stream.CloseSend()

					peerTask(stream)
				}()
			}

			cfg.wg.Wait()
			lis.Close() // Exit the accept loop
		}()
	}

	close(cfg.allDone) // Shutdown the GRPC server
}

func (cfg *Reversal) chaincode(rootCA *x509.CertPool, clientKeyPair *tls.Certificate, clientCert *x509.CertPool) {
	cfg.allDone = make(chan struct{}, 0)

	cfg.handler = func(stream ChatStream) {
		chaincodeTask(stream)
		cfg.wg.Done()
	}

	grpcServer := grpc.NewServer()
	RegisterChatServer(grpcServer, cfg)

	lis := &ReversalListener{connc: make(chan net.Conn)}
	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	config := tls.Config{
		RootCAs:      rootCA,
		Certificates: []tls.Certificate{*clientKeyPair},
	}
	config.BuildNameToCertificate()

	conn, err := tls.Dial("tcp", fmt.Sprintf("localhost:%d", cfg.port), &config)
	if err != nil {
		log.Fatalf("chaincode: tls.Dial: %+v", err)
	}
	lis.connc <- conn

	<-cfg.allDone
}

type ReversalListener struct {
	connc chan net.Conn
}

func (lis *ReversalListener) Accept() (net.Conn, error) {
	ret, ok := <-lis.connc
	if ok {
		return ret, nil
	}
	return nil, errors.New("connc closed")
}

func (lis *ReversalListener) Close() error {
	close(lis.connc)
	return nil
}

func (*ReversalListener) Addr() net.Addr {
	return nil
}
