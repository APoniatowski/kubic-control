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

package kubicctl

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"os"

	homedir "github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/thkukuk/kubic-control/pkg/rbac"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"gopkg.in/ini.v1"
)

const (
	root_crtFile = "/etc/kubicd/pki/admin.crt"
	root_keyFile = "/etc/kubicd/pki/admin.key"
	root_caFile  = "/etc/kubicd/pki/Kubic-Control-CA.crt"
)

var (
	Version    = "unreleased"
	servername = "localhost"
	port       = "7148"

	usercfg = "~/.config/kubicctl/kubicctl.conf"

	// Client Certificates
	crtFile = "~/.config/kubicctl/user.crt"
	keyFile = "~/.config/kubicctl/user.key"
	caFile  = "~/.config/kubicctl/Kubic-Control-CA.crt"
)

// exists returns whether the given file or directory exists
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}

func Execute() error {

	homecfg, err := homedir.Expand(usercfg)
	if err == nil {
		cfg, cfg_err := ini.LooseLoad(homecfg)
		if cfg_err, ok := cfg_err.(*os.PathError); ok {
			log.Fatal(cfg_err)
		}
		if cfg.Section("global").HasKey("server") {
			servername = cfg.Section("global").Key("server").String()
		}
		if cfg.Section("global").HasKey("port") {
			port = cfg.Section("global").Key("port").String()
		}
	}

	// if called as root, use admin certificates as default if local
	// ones don't exit
	if os.Getuid() == 0 {
		crt, err := homedir.Expand(crtFile)
		if err == nil {
			found, _ := exists(crt)
			if found == false {
				crtFile = root_crtFile
				keyFile = root_keyFile
				caFile = root_caFile
			}
		}
	}

	rootCmd := &cobra.Command{
		Use:   "kubicctl",
		Short: "Kubic Control  Daemon Interface"}

	rootCmd.Version = Version
	rootCmd.PersistentFlags().StringVarP(&servername, "server", "s", servername, "Name of server kubicd is running on")
	rootCmd.PersistentFlags().StringVarP(&port, "port", "p", port, "Port on which kubicd is listening")
	rootCmd.PersistentFlags().StringVar(&crtFile, "crtfile", crtFile, "Certificate with the public key for the user")
	rootCmd.PersistentFlags().StringVar(&keyFile, "keyfile", keyFile, "Private key for the user")
	rootCmd.PersistentFlags().StringVar(&caFile, "cafile", caFile, "Certificate with the public key of the CA for the server certificate")
	rootCmd.AddCommand(
		VersionCmd(),
		InitMasterCmd(),
		NodeCmd(),
		UpgradeKubernetesCmd(),
		FetchKubeconfigCmd(),
		CertificatesCmd(),
		DestroyClusterCmd(),
		rbac.RBACCmd(),
		GetStatusCmd(),
		DeployCmd(),
	)

	crtFile, err = homedir.Expand(crtFile)
	if err != nil {
		log.Fatal(err)
	}
	keyFile, err = homedir.Expand(keyFile)
	if err != nil {
		log.Fatal(err)
	}
	caFile, err = homedir.Expand(caFile)
	if err != nil {
		log.Fatal(err)
	}

	if err := rootCmd.Execute(); err != nil {
		// log.Fatal(err)
		return err
	}

	return nil
}

func CreateConnection() (*grpc.ClientConn, error) {
	// Load the certificates from disk
	certificate, err := tls.LoadX509KeyPair(crtFile, keyFile)
	if err != nil {
		log.Errorf("could not load client key pair: %s", err)
		return nil, err
	}

	// Create a certificate pool from the certificate authority
	certPool := x509.NewCertPool()
	ca, err := ioutil.ReadFile(caFile)
	if err != nil {
		log.Errorf("could not read ca certificate: %s", err)
		return nil, err
	}

	// Append the client certificates from the CA
	if ok := certPool.AppendCertsFromPEM(ca); !ok {
		log.Error("failed to append ca pki")
		return nil, err
	}

	// Create the TLS credentials for transport
	creds := credentials.NewTLS(&tls.Config{
		ServerName:   "KubicD",
		Certificates: []tls.Certificate{certificate},
		RootCAs:      certPool,
	})

	conn, err := grpc.Dial(servername+":"+port, grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Errorf("did not connect: %v", err)
		return nil, err
	}

	return conn, nil
}
