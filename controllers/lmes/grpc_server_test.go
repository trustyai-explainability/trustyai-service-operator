/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package lmes

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
)

var _ = Describe("GRPC Server", func() {
	Context("Start the GRPC Server", func() {

		ctx := context.Background()
		BeforeEach(func() {
			By("create ca, key, cert for the test case")
			ca := &x509.Certificate{
				SerialNumber: big.NewInt(2019),
				Subject: pkix.Name{
					Organization:  []string{"LM-Eval"},
					Country:       []string{"US"},
					Province:      []string{""},
					Locality:      []string{"San Jose"},
					StreetAddress: []string{"large lanaguage"},
					PostalCode:    []string{"95141"},
				},
				NotBefore:             time.Now(),
				NotAfter:              time.Now().AddDate(10, 0, 0),
				IsCA:                  true,
				ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
				KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
				BasicConstraintsValid: true,
			}
			caPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
			Expect(err).NotTo(HaveOccurred())
			caBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
			Expect(err).NotTo(HaveOccurred())
			caPEM := new(bytes.Buffer)
			pem.Encode(caPEM, &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: caBytes,
			})

			caPrivKeyPEM := new(bytes.Buffer)
			pem.Encode(caPrivKeyPEM, &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey),
			})

			cert := &x509.Certificate{
				SerialNumber: big.NewInt(1658),
				Subject: pkix.Name{
					Organization:  []string{"LM-Eval"},
					Country:       []string{"US"},
					Province:      []string{""},
					Locality:      []string{"San Jose"},
					StreetAddress: []string{"large lanaguage"},
					PostalCode:    []string{"95141"},
				},
				IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
				NotBefore:    time.Now(),
				NotAfter:     time.Now().AddDate(10, 0, 0),
				SubjectKeyId: []byte{1, 2, 3, 4, 6},
				ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
				KeyUsage:     x509.KeyUsageDigitalSignature,
			}

			certPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
			Expect(err).NotTo(HaveOccurred())

			certBytes, err := x509.CreateCertificate(rand.Reader, cert, ca, &certPrivKey.PublicKey, caPrivKey)
			Expect(err).NotTo(HaveOccurred())

			certPEM := new(bytes.Buffer)
			pem.Encode(certPEM, &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: certBytes,
			})

			certPrivKeyPEM := new(bytes.Buffer)
			pem.Encode(certPrivKeyPEM, &pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
			})

			var writeToFile = func(prefix string, content *bytes.Buffer, env string) {
				tmpFile, err := os.CreateTemp("", prefix)
				Expect(err).NotTo(HaveOccurred())
				_, err = tmpFile.Write(content.Bytes())
				Expect(err).NotTo(HaveOccurred())
				viper.Set(env, tmpFile.Name())
			}

			writeToFile("ca", caPEM, GrpcClientCaEnv)
			writeToFile("cert", certPEM, GrpcServerCertEnv)
			writeToFile("key", certPrivKeyPEM, GrpcServerKeyEnv)
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LMEvalJobReconciler{
				options: &ServiceOptions{
					GrpcPort:    8082,
					GrpcService: "localhost",
				},
			}

			go StartGrpcServer(ctx, controllerReconciler)
			time.Sleep(time.Second * 10)
			server.Stop()
		})
	})
})
