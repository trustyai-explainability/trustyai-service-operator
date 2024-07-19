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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"github.com/spf13/viper"
	"github.com/trustyai-explainability/trustyai-service-operator/controllers/lmes/api/v1beta1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var server *grpc.Server

func StartGrpcServer(ctx context.Context, ctor *LMEvalJobReconciler) error {
	log := log.FromContext(ctx)
	// checking the cer/key envs
	if viper.IsSet(GrpcServerCertEnv) && viper.IsSet(GrpcServerKeyEnv) {
		serverKey, serverCert := viper.GetString(GrpcServerKeyEnv), viper.GetString(GrpcServerCertEnv)

		if viper.IsSet(GrpcClientCaEnv) {
			// mTLS case
			var err error
			if server, err = getMTLSServer(serverCert, serverKey); err != nil {
				return err
			}
			ctor.options.grpcTLSMode = TLSMode_mTLS
			log.Info("GRPC server uses the mTLS")
		} else {
			// TLS case
			creds, err := credentials.NewServerTLSFromFile(serverCert, serverKey)
			if err != nil {
				return err
			}
			server = grpc.NewServer(grpc.Creds(creds))
			ctor.options.grpcTLSMode = TLSMode_TLS
			log.Info("GRPC server uses the TLS")
		}
	} else {
		ctor.options.grpcTLSMode = TLSMode_None
		log.Info("GRPC server uses insecure protocol")
		server = grpc.NewServer()
	}

	v1beta1.RegisterLMEvalJobUpdateServiceServer(server, &updateStatusServer{
		controller: ctor,
	})
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", ctor.options.GrpcPort))
	if err != nil {
		return err
	}
	log.Info("GRPC server started")
	err = server.Serve(lis)
	return err
}

func getMTLSServer(serverCert string, serverKey string) (*grpc.Server, error) {
	clientCaPath := viper.GetString(GrpcClientCaEnv)
	cert, err := tls.LoadX509KeyPair(serverCert, serverKey)
	if err != nil {
		return nil, err
	}
	ca := x509.NewCertPool()
	caBytes, err := os.ReadFile(clientCaPath)
	if err != nil {
		return nil, err
	}
	if ok := ca.AppendCertsFromPEM(caBytes); !ok {
		return nil, fmt.Errorf("failed to parse %q", clientCaPath)
	}
	tlsConfig := &tls.Config{
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    ca,
	}

	return grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig))), nil
}

type updateStatusServer struct {
	v1beta1.UnimplementedLMEvalJobUpdateServiceServer
	controller *LMEvalJobReconciler
}

func (s *updateStatusServer) UpdateStatus(ctx context.Context, newStatus *v1beta1.JobStatus) (resp *v1beta1.Response, err error) {
	resp = &v1beta1.Response{
		Code:    v1beta1.ResponseCode_OK,
		Message: "updated the job status successfully",
	}

	err = s.controller.updateStatus(ctx, newStatus)
	if err != nil {
		resp.Code = v1beta1.ResponseCode_ERROR
		resp.Message = err.Error()
	}

	return resp, err
}
