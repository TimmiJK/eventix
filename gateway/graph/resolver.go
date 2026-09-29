package graph

import grpcclients "eventix/gateway/internal/grpc_clients"

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

type Resolver struct {
	Clients *grpcclients.Clients
}

func NewResolver(clients *grpcclients.Clients) *Resolver {
	return &Resolver{
		Clients: clients,
	}
}
