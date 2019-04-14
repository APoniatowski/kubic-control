// Copyright 2019 Thorsten Kukuk
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"net"
	"time"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/codes"
	"gopkg.in/ini.v1"
	"github.com/spf13/cobra"
	log "github.com/sirupsen/logrus"
	"github.com/thkukuk/kubic-control/pkg/kubeadm"
	pb "github.com/thkukuk/kubic-control/api"
)

var (
	Version = "unreleased"
	servername = "localhost"
	port = "7148"
	crtFile = "/etc/kubicd/pki/KubicD.crt"
	keyFile = "/etc/kubicd/pki/KubicD.key"
	caFile = "/etc/kubicd/pki/Kubic-Control-CA.crt"
	rbac, rbac_err = ini.LooseLoad("/usr/share/defaults/kubicd/rbac.conf", "/etc/kubicd/rbac.conf")
	cfg, cfg_err = ini.LooseLoad("/usr/share/defaults/kubicd/kubicd.conf", "/etc/kubicd/kubicd.conf")
)

type server struct{}

func (s *server) InitMaster(in *pb.InitRequest, stream pb.Kubeadm_InitMasterServer) error {
	return kubeadm.InitMaster(in, stream)
}

func (s *server) AddNode(ctx context.Context, in *pb.AddNodeRequest) (*pb.StatusReply, error) {
	log.Printf("Received: add node  %v", in.NodeNames)
	status, message := kubeadm.AddNode(in.NodeNames)
	return &pb.StatusReply{Success: status, Message: message}, nil
}

func (s *server) RemoveNode(ctx context.Context, in *pb.RemoveNodeRequest) (*pb.StatusReply, error) {
	log.Printf("Received: remove node  %v", in.NodeNames)
	status, message := kubeadm.RemoveNode(in.NodeNames)
	return &pb.StatusReply{Success: status, Message: message}, nil
}

func (s *server) RebootNode(ctx context.Context, in *pb.RebootNodeRequest) (*pb.StatusReply, error) {
	log.Printf("Received: reboot node  %v", in.NodeNames)
	status, message := kubeadm.RebootNode(in.NodeNames)
	return &pb.StatusReply{Success: status, Message: message}, nil
}

func (s *server) UpgradeKubernetes(ctx context.Context, in *pb.Version) (*pb.StatusReply, error) {
	log.Printf("Received: upgrade Kubernetes  %v", in.Version)
	status, message := kubeadm.UpgradeKubernetes(in.Version)
	return &pb.StatusReply{Success: status, Message: message}, nil
}

func (s *server) FetchKubeconfig(ctx context.Context, in *pb.Empty) (*pb.StatusReply, error) {
	log.Printf("Received: fetch kubeconfig")
	status, message := kubeadm.FetchKubeconfig()
	return &pb.StatusReply{Success: status, Message: message}, nil
}

func AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no peer found")
	}
	tlsAuth, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unexpected peer transport credentials")
	}
	if len(tlsAuth.State.VerifiedChains) == 0 || len(tlsAuth.State.VerifiedChains[0]) == 0 {
		return nil, status.Error(codes.Unauthenticated, "could not verify peer certificate")
	}
	// Check subject common name against configured username
	log.Info(tlsAuth.State.VerifiedChains[0][0].Subject.CommonName)

	start := time.Now()
	// Calls the handler
	h, err := handler(ctx, req)

	log.Infof("Function: %s, Caller: %s, Duration: %s, Error: %v",
		info.FullMethod,
		tlsAuth.State.VerifiedChains[0][0].Subject.CommonName,
		time.Since(start), err)

	return h, err
}

func loadConfigFile() {
	if cfg_err, ok := cfg_err.(*os.PathError); ok {
		log.Fatal(cfg_err)
	}

	if cfg.Section("global").HasKey("crtfile") {
		crtFile =  cfg.Section("global").Key("crtfile").String()
	}
	if cfg.Section("global").HasKey("keyfile") {
		keyFile =  cfg.Section("global").Key("keyfile").String()
	}
	if cfg.Section("global").HasKey("cafile") {
		caFile =  cfg.Section("global").Key("cafile").String()
	}
	if cfg.Section("global").HasKey("server") {
		servername =  cfg.Section("global").Key("server").String()
	}
	if cfg.Section("global").HasKey("port") {
		port =  cfg.Section("global").Key("port").String()
	}
}

func main() {
	loadConfigFile()

	rootCmd := &cobra.Command{
                Use:   "kubicd",
                Short: "Kubic Control  Daemon",
                Run:   kubicd,
	        Args: cobra.ExactArgs(0),
	}

	rootCmd.Version = Version
        rootCmd.PersistentFlags().StringVarP(&servername, "server", "s", servername, "Servername kubicd is listening on")
        rootCmd.PersistentFlags().StringVarP(&port, "port", "p", port, "Port on which kubicd is listening")
        rootCmd.PersistentFlags().StringVar(&crtFile, "crtfile", crtFile, "Certificate with the public key for the daemon")
        rootCmd.PersistentFlags().StringVar(&keyFile, "keyfile", keyFile, "Private key for the daemon")
        rootCmd.PersistentFlags().StringVar(&caFile, "cafile", caFile, "Certificate with the public key of the CA for the server certificate")

	if err := rootCmd.Execute(); err != nil {
                os.Exit (1)
        }
}

func kubicd(cmd *cobra.Command, args []string) {
        log.Infof("Kubic Daemon: %s", Version)

	// Load the certificates from disk
	certificate, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		log.Fatalf("could not load server key pair: %s", err)
	}

	// Create a certificate pool from the certificate authority
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(caFile)
	if err != nil {
		log.Fatalf("could not read ca certificate: %s", err)
	}

	// Append the client certificates from the CA
	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		log.Fatal("failed to append client certs")
	}

	lis, err := net.Listen("tcp", servername + ":" + port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Create the TLS credentials
	creds := credentials.NewTLS(&tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    certPool,
	})

	s := grpc.NewServer(grpc.Creds(creds),
		grpc.UnaryInterceptor(AuthInterceptor))
	pb.RegisterKubeadmServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
