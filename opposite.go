package main

import (
	"crypto/tls"
	"crypto/x509"
	fmt "fmt"
	"log"
	"net"
	"sync"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Opposite struct {
	ChatConfig
	port int
}

func (cfg *Opposite) peer(rootCA *x509.CertPool) {
	creds := credentials.NewClientTLSFromCert(rootCA, "")

	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", cfg.port), grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	defer conn.Close()

	cfg.wg = sync.WaitGroup{}

	// This loop simulates concurrent transaction requests comming from Fablic clients
	for i := 0; i < 128; i++ {
		cfg.wg.Add(1)
		go func() {
			client := NewChatClient(conn)
			stream, err := client.Hello(context.Background())
			if err != nil {
				log.Fatalf("%v.Hello(_) = _, %v", client, err)
			}
			defer stream.CloseSend()

			peerTask(stream)
		}()
	}

	cfg.wg.Wait()
	close(cfg.allDone) // Shutdown the GRPC server
}

func (cfg *Opposite) chaincode(keyPair *tls.Certificate) {
	cfg.allDone = make(chan struct{}, 0)

	lis, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	cfg.handler = func(stream ChatStream) {
		chaincodeTask(stream)
		cfg.wg.Done()
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(keyPair)))
	RegisterChatServer(grpcServer, cfg)

	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	<-cfg.allDone
}
