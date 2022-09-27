package cmd

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"

	auth "github.com/formancehq/auth/pkg"
	"github.com/formancehq/auth/pkg/api"
	"github.com/formancehq/auth/pkg/api/accesscontrol"
	"github.com/formancehq/auth/pkg/delegatedauth"
	"github.com/formancehq/auth/pkg/oidc"
	"github.com/formancehq/auth/pkg/storage/sqlstorage"
	"github.com/gorilla/mux"
	sharedhealth "github.com/numary/go-libs/sharedhealth/pkg"
	"github.com/numary/go-libs/sharedlogging"
	"github.com/numary/go-libs/sharedotlp/pkg/sharedotlptraces"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/fx"
)

const (
	httpBindAddressFlag       = "http-bind-address"
	postgresUriFlag           = "postgres-uri"
	delegatedClientIDFlag     = "delegated-client-id"
	delegatedClientSecretFlag = "delegated-client-secret"
	delegatedIssuerFlag       = "delegated-issuer"
	baseUrlFlag               = "base-url"
	signingKeyFlag            = "signing-key"
	configFlag                = "config"

	DefaultSigningKey = `
-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEAth3atoCXldJgHH9EWnZQMvw5O+vVNKMcvrllEGQsLxvIA5xy
YPnFt2xU7k1dcN5ViBqPiigVHZNeyyHcdVclg26zqjEwYUqH+OPiRFeBn0SwOG+d
ZLpOIJdKt7OjmUG0xN9egq81dbPVPBPckuWqB9XMWmM+dtqydBX4lekj+Q1hFn5E
WXXuAs9aLIc8DzPz8B+oqwLKZ6k6kC5vpj+EaBt8ExVywrWftkWewGWRO7fLw0Fj
7hamaA1ZTYEqCN+MLDLEd6qmtC2cdgVhZM0RG2OnTiq5lGzNFmLXGOsquc35HSQj
OQqcLL+e/72K3giJ1YCqYWAIIJcc/kNKU8HtpwIDAQABAoIBAFY+dSEQbLjq09Er
A/fDJ9+9Sm1yFZnD1Q0NRysoBTSZ93KeWBxMrLFcgCwKP0IASIkX6voGWVmUPMP9
2SVIi99eQX9LpBmu7g2T/cdXmW8PXFSdpu/Yur78ZsnwLH2bfDvvfBZvWuXOsCCv
VznJwWfMe+YiMaafkvsenIaBziNWwUeVGHCWl5f++KGGbWFZjhkRZyjKWfMYflig
EG5e+WaXagCjTah5pUkmvLj3jmB1iGA/Askm8S5QyTt6Z+SIEk+i5T3qCiLFNvzp
7OeSyfbmWWzBYTiSvEoHhaHfdeicUyOpRthc33bb7LnfIWDG3Z+WE0o6U1nR8o7U
t5dsj2ECgYEA7SEuBpd/3wdNVLQSI/RHKKO3sdlymh7yRFf7OAn/UxnSJbSNx4y4
GAEdJD9KwSQlyekLITF+xc0IuyFHOmvuzp1+/LxK/QTY4dcdlwl/r1kmwBbTeR0e
yl9RtulHXmP+Ss/PZgwR081Lk7zlRkh1busyAOmCE4mJW/IvNBze0dsCgYEAxJvy
PcbaLVk497U9cUGznsSbbsyq7JGLkBgTu3eQ/yRgoE7pvagF7dV1gQGuCYjOaYml
U4d95FLPoiE+CE0g2uyouFEsD1UhggTADP33BidUKUcF1ub9VVNcWs4I5LeWPY/X
5vcpOCAkmRZWT5rieAECdIsfRTnePVyn2L7amyUCgYEAqsZAfWLSJm791Eiy383n
CW+OtbjiffhXhbzPIbaheNmZrKnxiYrgcfkrYZVrYtmDlXwOFeOtZwqYhRwcTgi5
PXfTonSAlOPOxibEGqgumrvb2m8V8Z11NU2cbdxnF6Vv17T9qoJ6vEyXZ1iczhcU
68LaiimhEiz1DZDHSgKYvg0CgYEAjVZyQXjXVWxjqKdQ4T9TKhq6hl95rJFA3DiC
zuy4fsKe9/9ixyWoBX7DdxdHDrGbeYErKa4okV/6xdnR51PS/67L55zq6KbRbM+P
ZIeZ8oGJXhchmoj5q0I/DUQ6Xnmf9ueWVQJvTlrFFIxbReTZU12ebzuoIjLkkgYu
34DsVEUCgYEAtHm/aO7/2UJT40PMO+VDvBCEixPtt6j72fLaW8btgVRAnhp9qaWX
Cv6TRZPe2y6Bbgg4Q3FuF0DMqx6ongFKQAWo3DkqNFCGRgjJMQ9JbcfOnGySq4U+
EL/wy5C80pa3jahniqVgO5L6zz0ZLtRIRE7aCtCIu826gctJ1+ShIso=
-----END RSA PRIVATE KEY-----
`
)

type ClientOptions struct {
	Clients []auth.ClientOptions `json:"clients" yaml:"clients"`
}

var serveCmd = &cobra.Command{
	Use: "serve",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return bindFlagsToViper(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if viper.GetString(baseUrlFlag) == "" {
			return errors.New("base url must be defined")
		}

		baseUrl, err := url.Parse(viper.GetString(baseUrlFlag))
		if err != nil {
			return errors.Wrap(err, "parsing base url")
		}

		delegatedClientID := viper.GetString(delegatedClientIDFlag)
		if delegatedClientID == "" {
			return errors.New("delegated client id must be defined")
		}

		delegatedClientSecret := viper.GetString(delegatedClientSecretFlag)
		if delegatedClientSecret == "" {
			return errors.New("delegated client secret must be defined")
		}

		delegatedIssuer := viper.GetString(delegatedIssuerFlag)
		if delegatedIssuer == "" {
			return errors.New("delegated issuer must be defined")
		}

		signingKey := viper.GetString(signingKeyFlag)
		if signingKey == "" {
			return errors.New("signing key must be defined")
		}

		block, _ := pem.Decode([]byte(signingKey))
		if block == nil {
			return errors.New("invalid signing key, cannot parse as PEM")
		}

		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return err
		}

		if viper.GetString(configFlag) != "" {
			viper.SetConfigFile(viper.GetString(configFlag))
			if err := viper.ReadInConfig(); err != nil {
				return errors.Wrap(err, "reading viper config file")
			}
		}

		o := ClientOptions{}
		if err := viper.Unmarshal(&o); err != nil {
			return errors.Wrap(err, "unmarshal viper config")
		}

		app := fx.New(
			AuthServerModule(cmd.Context(), baseUrl,
				viper.GetString(httpBindAddressFlag),
				viper.GetString(postgresUriFlag), key, o,
				delegatedIssuer, delegatedClientID, delegatedClientSecret))

		if err := app.Start(cmd.Context()); err != nil {
			return err
		}

		<-app.Done()

		return app.Err()
	},
}

func AuthServerModule(ctx context.Context, baseUrl *url.URL, bindAddr, postgresUri string, key *rsa.PrivateKey, o ClientOptions,
	delegatedIssuer, delegatedClientID, delegatedClientSecret string) fx.Option {
	options := []fx.Option{
		fx.Supply(fx.Annotate(ctx, fx.As(new(context.Context)))),
		api.Module(bindAddr, baseUrl),
		oidc.Module(key, baseUrl),
		accesscontrol.Module(),
		fx.Invoke(func(router *mux.Router, healthController *sharedhealth.HealthController) {
			router.Path("/_healthcheck").HandlerFunc(healthController.Check)
		}),
		sqlstorage.Module(postgresUri, key, o.Clients),
		delegatedauth.Module(delegatedauth.Config{
			Issuer:       delegatedIssuer,
			ClientID:     delegatedClientID,
			ClientSecret: delegatedClientSecret,
			RedirectURL:  fmt.Sprintf("%s/authorize/callback", baseUrl),
		}),
		fx.Invoke(func() {
			sharedlogging.Infof("App started.")
		}),
		fx.NopLogger,
	}

	if tm := sharedotlptraces.CLITracesModule(viper.GetViper()); tm != nil {
		options = append(options, tm)
	}

	return fx.Options(options...)
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().String(httpBindAddressFlag, ":8080", "Server HTTP bind address")
	serveCmd.Flags().String(postgresUriFlag, "", "Postgres uri")
	serveCmd.Flags().String(delegatedIssuerFlag, "", "Delegated OIDC issuer")
	serveCmd.Flags().String(delegatedClientIDFlag, "", "Delegated OIDC client id")
	serveCmd.Flags().String(delegatedClientSecretFlag, "", "Delegated OIDC client secret")
	serveCmd.Flags().String(baseUrlFlag, "http://localhost:8080", "Base service url")
	serveCmd.Flags().String(signingKeyFlag, DefaultSigningKey, "Signing key")

	serveCmd.Flags().String(configFlag, "config", "Config file name without extension")

	sharedotlptraces.InitOTLPTracesFlags(serveCmd.Flags())
}
