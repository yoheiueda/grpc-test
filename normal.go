package main

import (
	"crypto/tls"
	"crypto/x509"
	fmt "fmt"
	"log"
	"net"

	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Normal struct {
	ChatConfig
	port int
}

func (cfg *Normal) peer(keyPair *tls.Certificate) {

	cfg.handler = func(stream ChatStream) {

		serialStream := serialStream{cs: stream}
		for i := 0; i < 128; i++ {
			cfg.wg.Add(1)
			go func() {
				peerTask(&serialStream)
				cfg.wg.Done()
			}()
		}
		cfg.wg.Wait()
		// Returning from this handler closes the stream
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(keyPair)))
	RegisterChatServer(grpcServer, cfg)

	lis, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	go grpcServer.Serve(lis)
	defer grpcServer.Stop()

	<-cfg.allDone
}

func (cfg *Normal) chaincode(rootCA *x509.CertPool) {

	opt := grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(rootCA, ""))
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", cfg.port), opt)
	if err != nil {
		log.Fatalf("fail to dial: %+v", err)
	}
	defer conn.Close()
	client := NewChatClient(conn)

	stream, err := client.Hello(context.Background())
	if err != nil {
		log.Fatalf("%v.Hello(_) = _, %v", client, err)
	}

	chaincodeTask(&serialStream{cs: stream})

	close(cfg.allDone) // Shutdown the GRPC server
}
