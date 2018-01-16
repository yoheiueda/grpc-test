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

type Sharding struct {
	ChatConfig
	port     int
	poolSize int
}

func (cfg *Sharding) peer(keyPair *tls.Certificate) {
	pool := make([]*serialStream, 0)
	poolLock := sync.Mutex{}

	poolWG := sync.WaitGroup{}
	poolWG.Add(32)

	cfg.allDone = make(chan struct{}, 0)
	cfg.handler = func(stream ChatStream) {

		serialStream := serialStream{cs: stream}

		poolLock.Lock()
		pool = append(pool, &serialStream)
		poolLock.Unlock()

		poolWG.Done()

		// Returning from this handler closes the stream
		<-cfg.allDone
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(keyPair)))
	RegisterChatServer(grpcServer, cfg)

	lis, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	go grpcServer.Serve(lis)
	defer grpcServer.Stop()
	close(cfg.ready) // Notify chaincode

	poolWG.Wait()

	for i := 0; i < 128; i++ {
		cfg.wg.Add(1)
		s := pool[i%len(pool)]
		go func() {
			peerTask(s)
			cfg.wg.Done()
		}()
	}

	cfg.wg.Wait()

	close(cfg.allDone)
}

func (cfg *Sharding) chaincode(rootCA *x509.CertPool) {

	<-cfg.ready

	opt := grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(rootCA, ""))
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", cfg.port), opt)
	if err != nil {
		log.Fatalf("fail to dial: %+v", err)
	}
	defer conn.Close()
	client := NewChatClient(conn)

	wg := sync.WaitGroup{}
	wg.Add(32)

	for i := 0; i < 32; i++ {
		stream, err := client.Hello(context.Background())
		if err != nil {
			log.Fatalf("%v.Hello(_) = _, %v", client, err)
		}
		go func() {
			chaincodeTask(&serialStream{cs: stream})
			wg.Done()
		}()
	}
	wg.Wait()
}
