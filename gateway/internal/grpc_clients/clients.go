package grpcclients

import (
	authPb "eventix/proto/auth/pb"
	catalogPb "eventix/proto/catalog/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Clients struct {
	Auth    authPb.AuthServiceClient
	Catalog catalogPb.CatalogServiceClient
}

func NewClients(authAddr, catalogAddr string) (*Clients, error) {
	authConn, err := grpc.NewClient(authAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	catalogConn, err := grpc.NewClient(catalogAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	return &Clients{
		Auth:    authPb.NewAuthServiceClient(authConn),
		Catalog: catalogPb.NewCatalogServiceClient(catalogConn),
	}, nil
}
