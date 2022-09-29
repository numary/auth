package oidc

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/zitadel/oidc/pkg/client"
	"go.formance.com/auth/pkg/delegatedauth"
	"gopkg.in/square/go-jose.v2"
)

func ReadKeySet(ctx context.Context, configuration delegatedauth.Config) (*jose.JSONWebKeySet, error) {
	// TODO: Inefficient, should keep public keys locally and use them instead of calling the network
	discoveryConfiguration, err := client.Discover(configuration.Issuer, http.DefaultClient)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryConfiguration.JwksURI, nil)
	if err != nil {
		return nil, err
	}

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	keySet := jose.JSONWebKeySet{}
	if err := json.NewDecoder(rsp.Body).Decode(&keySet); err != nil {
		return nil, err
	}

	return &keySet, nil
}
