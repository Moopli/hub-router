/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package startcmd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gorilla/mux"
	"github.com/hyperledger/aries-framework-go-ext/component/storage/mongodb"
	"github.com/hyperledger/aries-framework-go-ext/component/storage/mysql"
	"github.com/hyperledger/aries-framework-go/component/storageutil/mem"
	arieslog "github.com/hyperledger/aries-framework-go/pkg/common/log"
	"github.com/hyperledger/aries-framework-go/pkg/controller/rest/kms"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/messaging/msghandler"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/packer"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/packer/anoncrypt"
	"github.com/hyperledger/aries-framework-go/pkg/didcomm/packer/authcrypt"
	legacyauthcrypt "github.com/hyperledger/aries-framework-go/pkg/didcomm/packer/legacy/authcrypt"
	arieshttp "github.com/hyperledger/aries-framework-go/pkg/didcomm/transport/http"
	ariesws "github.com/hyperledger/aries-framework-go/pkg/didcomm/transport/ws"
	"github.com/hyperledger/aries-framework-go/pkg/doc/jose"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries/api"
	"github.com/hyperledger/aries-framework-go/pkg/framework/aries/defaults"
	"github.com/hyperledger/aries-framework-go/pkg/framework/context"
	kms2 "github.com/hyperledger/aries-framework-go/pkg/kms"
	"github.com/hyperledger/aries-framework-go/spi/storage"
	"github.com/rs/cors"
	"github.com/spf13/cobra"
	"github.com/trustbloc/edge-core/pkg/log"
	cmdutils "github.com/trustbloc/edge-core/pkg/utils/cmd"
	tlsutils "github.com/trustbloc/edge-core/pkg/utils/tls"

	aries2 "github.com/trustbloc/hub-router/pkg/aries"
	"github.com/trustbloc/hub-router/pkg/restapi/operation"
)

// Network config.
const (
	hostURLFlagName      = "host-url"
	hostURLFlagShorthand = "u"
	hostURLFlagUsage     = "URL to run the hub-router instance on. Format: HostName:Port." +
		" Alternatively, this can be set with the following environment variable: " + hostURLEnvKey
	hostURLEnvKey = "HUB_ROUTER_HOST_URL"

	// didcomm http host internal url.
	didCommHTTPHostFlagName  = "didcomm-http-host"
	didCommHTTPHostEnvKey    = "HUB_ROUTER_DIDCOMM_HTTP_HOST"
	didCommHTTPHostFlagUsage = "DIDComm HTTP Host Name:Port. This is used internally to start the didcomm server." +
		" Alternatively, this can be set with the following environment variable: " + didCommHTTPHostEnvKey

	// didcomm http host external url.
	didCommHTTPHostExternalFlagName  = "didcomm-http-host-external"
	didCommHTTPHostExternalEnvKey    = "HUB_ROUTER_DIDCOMM_HTTP_HOST_EXTERNAL"
	didCommHTTPHostExternalFlagUsage = "DIDComm HTTP External Name." +
		" This is the URL for the inbound server as seen externally." +
		" If not provided, then the internal inbound host will be used here." +
		" Alternatively, this can be set with the following environment variable: " + didCommHTTPHostExternalEnvKey

	// didcomm websocket host internal url.
	didCommWSHostFlagName  = "didcomm-ws-host"
	didCommWSHostEnvKey    = "HUB_ROUTER_DIDCOMM_WS_HOST"
	didCommWSHostFlagUsage = "DIDComm WebSocket Host Name:Port. This is used internally to start the didcomm server." +
		" Alternatively, this can be set with the following environment variable: " + didCommWSHostEnvKey

	// didcomm websocket host external url.
	didCommWSHostExternalFlagName  = "didcomm-ws-host-external"
	didCommWSHostExternalEnvKey    = "HUB_ROUTER_DIDCOMM_WS_HOST_EXTERNAL"
	didCommWSHostExternalFlagUsage = "DIDComm WebSocket External Name." +
		" This is the URL for the inbound server as seen externally." +
		" If not provided, then the internal inbound host will be used here." +
		" Alternatively, this can be set with the following environment variable: " + didCommWSHostExternalEnvKey

	tlsSystemCertPoolFlagName  = "tls-systemcertpool"
	tlsSystemCertPoolFlagUsage = "Use system certificate pool." +
		" Possible values [true] [false]. Defaults to false if not set." +
		" Alternatively, this can be set with the following environment variable: " + tlsSystemCertPoolEnvKey
	tlsSystemCertPoolEnvKey = "HUB_ROUTER_TLS_SYSTEMCERTPOOL"

	tlsCACertsFlagName  = "tls-cacerts"
	tlsCACertsFlagUsage = "Comma-Separated list of ca certs path." +
		" Alternatively, this can be set with the following environment variable: " + tlsCACertsEnvKey
	tlsCACertsEnvKey = "HUB_ROUTER_TLS_CACERTS"

	tlsServeCertPathFlagName  = "tls-serve-cert"
	tlsServeCertPathFlagUsage = "Path to the server certificate to use when serving HTTPS." +
		" Alternatively, this can be set with the following environment variable: " + tlsServeCertPathEnvKey
	tlsServeCertPathEnvKey = "HUB_ROUTER_TLS_SERVE_CERT"

	tlsServeKeyPathFlagName  = "tls-serve-key"
	tlsServeKeyPathFlagUsage = "Path to the private key to use when serving HTTPS." +
		" Alternatively, this can be set with the following environment variable: " + tlsServeKeyPathFlagEnvKey
	tlsServeKeyPathFlagEnvKey = "HUB_ROUTER_TLS_SERVE_KEY"
)

// DIDComm config.
const (
	didcommV2FlagName  = "use-didcomm-v2"
	didcommV2FlagUsage = "Use DIDComm V2. Possible values [true] [false]. Defaults to false if not set." +
		" Alternatively, this can be set with the following environment variable: " + didcommV2EnvKey
	didcommV2EnvKey = "HUB_ROUTER_DIDCOMM_V2"

	// default verification key type flag.
	keyTypeFlagName = "key-type"
	keyTypeEnvKey   = "HUB_ROUTER_KEY_TYPE"
	keyTypeUsage    = "Default key type for router." +
		" This flag sets the verification (and for DIDComm V1 encryption as well) key type used for key creation " +
		"in the router. Alternatively, this can be set with the following environment variable: " +
		keyTypeEnvKey

	// default key agreement type flag.
	keyAgreementTypeFlagName = "key-agreement-type"
	keyAgreementTypeEnvKey   = "HUB_ROUTER_KEY_AGREEMENT_TYPE"
	keyAgreementTypeUsage    = "Default key agreement type for router." +
		" Default encryption (used in DIDComm V2) key type used for key agreement creation in the router." +
		" Alternatively, this can be set with the following environment variable: " +
		keyAgreementTypeEnvKey
)

//  Public DID config
const (
	orbDomainFlagName  = "orb-domain"
	orbDomainFlagUsage = "Domain for the orb DID method instance where a public DID should be created." +
		" Alternatively, this can be set with the following environment variable: " + orbDomainEnvKey
	orbDomainEnvKey = "HUB_ROUTER_ORB_DOMAIN"

	orbAnchorOriginFlagName  = "orb-anchor-origin"
	orbAnchorOriginFlagUsage = "Orb DID method anchor origin for the router's DID. Optional." +
		" Alternatively, this can be set with the following environment variable: " + orbAnchorOriginEnvKey
	orbAnchorOriginEnvKey = "HUB_ROUTER_ORB_ANCHOR_ORIGIN"

	orbOperationEndpointsFlagName  = "orb-operation-endpoints"
	orbOperationEndpointsFlagUsage = "Comma-separated list of orb DID method operation endpoints. Optional." +
		" Alternatively, this can be set with the following environment variable: " + orbOperationEndpointsEnvKey
	orbOperationEndpointsEnvKey = "HUB_ROUTER_ORB_OPERATION_ENDPOINTS"
)

// Storage config.
const (
	storagePrefix = "hubrouter"

	datasourcePersistentFlagName  = "dsn-p"
	datasourcePersistentFlagUsage = "Persistent datasource Name with credentials if required." +
		" Format must be <driver>:[//]<driver-specific-dsn>." +
		" Examples: 'mysql://root:secret@tcp(localhost:3306)/hubrouter', 'mem://test'," +
		" 'mongodb://mongodb.example.com:27017'." +
		" Supported drivers are [mem, mysql, mongodb]." +
		" Alternatively, this can be set with the following environment variable: " + datasourcePersistentEnvKey
	datasourcePersistentEnvKey = "HUB_ROUTER_DSN_PERSISTENT"

	datasourceTransientFlagName  = "dsn-t"
	datasourceTransientFlagUsage = "Datasource Name with credentials if required." +
		" Format must be <driver>:[//]<driver-specific-dsn>." +
		" Examples: 'mysql://root:secret@tcp(localhost:3306)/hubrouter', 'mem://test'," +
		" 'mongodb://mongodb.example.com:27017'." +
		" Supported drivers are [mem, mysql, mongodb]." +
		" Alternatively, this can be set with the following environment variable: " + datasourceTransientEnvKey
	datasourceTransientEnvKey = "HUB_ROUTER_DSN_TRANSIENT"

	datasourceTimeoutFlagName  = "dsn-timeout"
	datasourceTimeoutFlagUsage = "Total time in seconds to wait until the datasource is available before giving up." +
		" Default: " + string(rune(datasourceTimeoutDefault)) + " seconds." +
		" Alternatively, this can be set with the following environment variable: " + datasourceTimeoutEnvKey
	datasourceTimeoutEnvKey  = "HUB_ROUTER_DSN_TIMEOUT"
	datasourceTimeoutDefault = 30
)

// "Other" bucket.
const (
	logLevelFlagName  = "log-level"
	logLevelFlagUsage = "Sets the logging level." +
		" Possible values are [DEBUG, INFO, WARNING, ERROR, CRITICAL] (default is INFO)." +
		" Alternatively, this can be set with the following environment variable: " + logLevelEnvKey
	logLevelEnvKey = "HUB_ROUTER_LOGLEVEL"
)

const (
	sleep = 1 * time.Second
)

// Database types.
const (
	databaseTypeMemOption     = "mem"
	databaseTypeMySQLOption   = "mysql"
	databaseTypeMongoDBOption = "mongodb"
)

var logger = log.New("hub-router")

// nolint:gochecknoglobals // we map the <driver> portion of datasource URLs to this map's keys
var supportedStorageProviders = map[string]func(string, string) (storage.Provider, error){
	databaseTypeMySQLOption: func(dsn, prefix string) (storage.Provider, error) {
		return mysql.NewProvider(dsn, mysql.WithDBPrefix(prefix))
	},
	databaseTypeMemOption: func(
		_, _ string) (storage.Provider, error) { // nolint:unparam // memstorage provider never returns error
		return mem.NewProvider(), nil
	},
	databaseTypeMongoDBOption: func(dsn, prefix string) (storage.Provider, error) {
		return mongodb.NewProvider(dsn, mongodb.WithDBPrefix(prefix))
	},
}

type tlsParameters struct {
	systemCertPool bool
	caCerts        []string
	serveCertPath  string
	serveKeyPath   string
}

type didCommParameters struct {
	httpHostInternal string
	httpHostExternal string
	wsHostInternal   string
	wsHostExternal   string
	useDIDCommV2     bool
	keyType          string
	keyAgreementType string
}

type datasourceParams struct {
	persistentURL string
	transientURL  string
	timeout       uint64
}

type hubRouterParameters struct {
	hostURL             string
	tlsParams           *tlsParameters
	datasourceParams    *datasourceParams
	didCommParameters   *didCommParameters
	orbClientParameters *orbClientParameters
}

type orbClientParameters struct {
	domain             string
	anchorOrigin       string
	operationEndpoints []string
}

type server interface {
	ListenAndServe(host string, router http.Handler) error

	ListenAndServeTLS(host, certFile, keyFile string, router http.Handler) error
}

// HTTPServer represents an actual HTTP server implementation.
type HTTPServer struct{}

// ListenAndServe starts the server using the standard Go HTTP implementation.
func (s *HTTPServer) ListenAndServe(host string, router http.Handler) error {
	return http.ListenAndServe(host, router)
}

// ListenAndServeTLS starts the server using the standard Go HTTPS implementation.
func (s *HTTPServer) ListenAndServeTLS(host, certFile, keyFile string, router http.Handler) error {
	return http.ListenAndServeTLS(host, certFile, keyFile, router)
}

// GetStartCmd returns the Cobra start command.
func GetStartCmd(srv server) *cobra.Command {
	startCmd := createStartCmd(srv)

	createFlags(startCmd)

	return startCmd
}

func createStartCmd(srv server) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start hub-router",
		Long:  "Start hub-router",
		RunE: func(cmd *cobra.Command, args []string) error {
			parameters, err := getHubRouterParameters(cmd)
			if err != nil {
				return err
			}

			return startHubRouter(parameters, srv)
		},
	}
}

func createFlags(startCmd *cobra.Command) {
	startCmd.Flags().StringP(hostURLFlagName, hostURLFlagShorthand, "", hostURLFlagUsage)
	startCmd.Flags().StringP(tlsSystemCertPoolFlagName, "", "", tlsSystemCertPoolFlagUsage)
	startCmd.Flags().StringArrayP(tlsCACertsFlagName, "", []string{}, tlsCACertsFlagUsage)
	startCmd.Flags().StringP(tlsServeCertPathFlagName, "", "", tlsServeCertPathFlagUsage)
	startCmd.Flags().StringP(tlsServeKeyPathFlagName, "", "", tlsServeKeyPathFlagUsage)
	startCmd.Flags().StringP(datasourcePersistentFlagName, "", "", datasourcePersistentFlagUsage)
	startCmd.Flags().StringP(datasourceTransientFlagName, "", "", datasourceTransientFlagUsage)
	startCmd.Flags().StringP(datasourceTimeoutFlagName, "", "", datasourceTimeoutFlagUsage)

	// didcomm
	startCmd.Flags().StringP(didCommHTTPHostFlagName, "", "", didCommHTTPHostFlagUsage)
	startCmd.Flags().StringP(didCommHTTPHostExternalFlagName, "", "", didCommHTTPHostExternalFlagUsage)
	startCmd.Flags().StringP(didCommWSHostFlagName, "", "", didCommWSHostFlagUsage)
	startCmd.Flags().StringP(didCommWSHostExternalFlagName, "", "", didCommWSHostExternalFlagUsage)
	startCmd.Flags().StringP(didcommV2FlagName, "", "", didcommV2FlagUsage)
	startCmd.Flags().StringP(keyTypeFlagName, "", "", keyTypeUsage)
	startCmd.Flags().StringP(keyAgreementTypeFlagName, "", "", keyAgreementTypeUsage)

	// orb client
	startCmd.Flags().StringP(orbDomainFlagName, "", "", orbDomainFlagUsage)
	startCmd.Flags().StringP(orbAnchorOriginFlagName, "", "", orbAnchorOriginFlagUsage)
	startCmd.Flags().StringArrayP(orbOperationEndpointsFlagName, "", []string{}, orbOperationEndpointsFlagUsage)

	startCmd.Flags().StringP(logLevelFlagName, "", "INFO", logLevelFlagUsage)
}

func getHubRouterParameters(cmd *cobra.Command) (*hubRouterParameters, error) {
	hostURL, err := cmdutils.GetUserSetVarFromString(cmd, hostURLFlagName, hostURLEnvKey, false)
	if err != nil {
		return nil, err
	}

	tlsParams, err := getTLS(cmd)
	if err != nil {
		return nil, err
	}

	dsParams, err := getDatasourceParams(cmd)
	if err != nil {
		return nil, err
	}

	// didcomm
	didCommParameters, err := getDIDCommParams(cmd)
	if err != nil {
		return nil, err
	}

	orbParams, err := getOrbClientParameters(cmd)
	if err != nil {
		return nil, err
	}

	logLevel, err := cmdutils.GetUserSetVarFromString(cmd, logLevelFlagName, logLevelEnvKey, true)
	if err != nil {
		return nil, err
	}

	if logLevel == "" {
		logLevel = "INFO"
	}

	err = setLogLevel(logLevel)
	if err != nil {
		return nil, err
	}

	logger.Infof("logger level set to %s", logLevel)

	return &hubRouterParameters{
		hostURL:             hostURL,
		tlsParams:           tlsParams,
		datasourceParams:    dsParams,
		didCommParameters:   didCommParameters,
		orbClientParameters: orbParams,
	}, nil
}

func getTLS(cmd *cobra.Command) (*tlsParameters, error) {
	tlsSystemCertPoolString, err := cmdutils.GetUserSetVarFromString(cmd, tlsSystemCertPoolFlagName,
		tlsSystemCertPoolEnvKey, true)
	if err != nil {
		return nil, err
	}

	tlsSystemCertPool := false
	if tlsSystemCertPoolString != "" {
		tlsSystemCertPool, err = strconv.ParseBool(tlsSystemCertPoolString)
		if err != nil {
			return nil, err
		}
	}

	tlsCACerts, err := cmdutils.GetUserSetVarFromArrayString(cmd, tlsCACertsFlagName, tlsCACertsEnvKey, true)
	if err != nil {
		return nil, err
	}

	tlsServeCertPath, err := cmdutils.GetUserSetVarFromString(cmd, tlsServeCertPathFlagName, tlsServeCertPathEnvKey, true)
	if err != nil {
		return nil, err
	}

	tlsServeKeyPath, err := cmdutils.GetUserSetVarFromString(cmd, tlsServeKeyPathFlagName, tlsServeKeyPathFlagEnvKey, true)
	if err != nil {
		return nil, err
	}

	return &tlsParameters{
		systemCertPool: tlsSystemCertPool,
		caCerts:        tlsCACerts,
		serveCertPath:  tlsServeCertPath,
		serveKeyPath:   tlsServeKeyPath,
	}, nil
}

func getDatasourceParams(cmd *cobra.Command) (*datasourceParams, error) {
	params := &datasourceParams{}

	var err error

	params.persistentURL, err = cmdutils.GetUserSetVarFromString(cmd,
		datasourcePersistentFlagName, datasourcePersistentEnvKey, false)
	if err != nil {
		return nil, err
	}

	params.transientURL, err = cmdutils.GetUserSetVarFromString(cmd,
		datasourceTransientFlagName, datasourceTransientEnvKey, false)
	if err != nil {
		return nil, err
	}

	timeout, err := cmdutils.GetUserSetVarFromString(cmd, datasourceTimeoutFlagName, datasourceTimeoutEnvKey, true)
	if err != nil && !strings.Contains(err.Error(), "value is empty") {
		return nil, fmt.Errorf("failed to configure dsn timeout: %w", err)
	}

	t := datasourceTimeoutDefault

	if timeout != "" {
		t, err = strconv.Atoi(timeout)
		if err != nil {
			return nil, fmt.Errorf("failed to parse dsn timeout %s: %w", timeout, err)
		}
	}

	params.timeout = uint64(t)

	return params, err
}

func getDIDCommParams(cmd *cobra.Command) (*didCommParameters, error) {
	httpHostInternal, err := cmdutils.GetUserSetVarFromString(cmd, didCommHTTPHostFlagName,
		didCommHTTPHostEnvKey, false)
	if err != nil {
		return nil, err
	}

	httpHostExternal, err := cmdutils.GetUserSetVarFromString(cmd, didCommHTTPHostExternalFlagName,
		didCommHTTPHostExternalEnvKey, true)
	if err != nil {
		return nil, err
	}

	wsHostInternal, err := cmdutils.GetUserSetVarFromString(cmd, didCommWSHostFlagName,
		didCommWSHostEnvKey, false)
	if err != nil {
		return nil, err
	}

	wsHostExternal, err := cmdutils.GetUserSetVarFromString(cmd, didCommWSHostExternalFlagName,
		didCommWSHostExternalEnvKey, true)
	if err != nil {
		return nil, err
	}

	keyType, err := cmdutils.GetUserSetVarFromString(cmd, keyTypeFlagName, keyTypeEnvKey, true)
	if err != nil {
		return nil, err
	}

	keyAgreementType, err := cmdutils.GetUserSetVarFromString(cmd, keyAgreementTypeFlagName, keyAgreementTypeEnvKey, true)
	if err != nil {
		return nil, err
	}

	useDIDCommV2String, err := cmdutils.GetUserSetVarFromString(cmd, didcommV2FlagName, didcommV2EnvKey, true)
	if err != nil {
		return nil, err
	}

	var useDIDCommV2 bool

	if useDIDCommV2String != "" {
		useDIDCommV2, err = strconv.ParseBool(useDIDCommV2String)
		if err != nil {
			return nil, fmt.Errorf("parsing use-didcomm-v2 flag: %w", err)
		}
	}

	return &didCommParameters{
		httpHostInternal: httpHostInternal,
		httpHostExternal: httpHostExternal,
		wsHostInternal:   wsHostInternal,
		wsHostExternal:   wsHostExternal,
		useDIDCommV2:     useDIDCommV2,
		keyType:          keyType,
		keyAgreementType: keyAgreementType,
	}, nil
}

func getOrbClientParameters(cmd *cobra.Command) (*orbClientParameters, error) {
	orbDomain, err := cmdutils.GetUserSetVarFromString(cmd, orbDomainFlagName,
		orbDomainEnvKey, false)
	if err != nil {
		return nil, err
	}

	orbAnchorOrigin, err := cmdutils.GetUserSetVarFromString(cmd, orbAnchorOriginFlagName,
		orbAnchorOriginEnvKey, true)
	if err != nil {
		return nil, err
	}

	orbOperationEndpoints, err := cmdutils.GetUserSetVarFromArrayString(cmd, orbOperationEndpointsFlagName,
		orbOperationEndpointsEnvKey, true)
	if err != nil {
		return nil, err
	}

	return &orbClientParameters{
		domain:             orbDomain,
		anchorOrigin:       orbAnchorOrigin,
		operationEndpoints: orbOperationEndpoints,
	}, nil
}

func setLogLevel(logLevel string) error {
	err := setEdgeCoreLogLevel(logLevel)
	if err != nil {
		return err
	}

	return setAriesFrameworkLogLevel(logLevel)
}

func setEdgeCoreLogLevel(logLevel string) error {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("failed to parse log level '%s' : %w", logLevel, err)
	}

	log.SetLevel("", level)

	return nil
}

func setAriesFrameworkLogLevel(logLevel string) error {
	level, err := arieslog.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("failed to parse log level '%s' : %w", logLevel, err)
	}

	arieslog.SetLevel("", level)

	return nil
}

func startHubRouter( // nolint:gocyclo // initialization apart from aries
	params *hubRouterParameters, srv server) error {
	switch {
	case params.tlsParams.serveCertPath != "" && params.tlsParams.serveKeyPath == "":
		return errors.New("cert path and key path are mandatory : missing key path")
	case params.tlsParams.serveCertPath == "" && params.tlsParams.serveKeyPath != "":
		return errors.New("cert path and key path are mandatory : missing cert path")
	}

	rootCAs, err := tlsutils.GetCertPool(params.tlsParams.systemCertPool, params.tlsParams.caCerts)
	if err != nil {
		return fmt.Errorf("get root CAs : %w", err)
	}

	msgRegistrar := msghandler.NewRegistrar()

	tlsConfig := &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12}

	framework, err := createAriesAgent(params, tlsConfig, msgRegistrar)
	if err != nil {
		return err
	}

	ctx, err := framework.Context()
	if err != nil {
		return fmt.Errorf("aries-framework - get aries context : %w", err)
	}

	publicDID := ""

	if params.didCommParameters.useDIDCommV2 {
		didCommEndpoint := params.didCommParameters.httpHostExternal
		if didCommEndpoint == "" {
			didCommEndpoint = params.didCommParameters.httpHostInternal
		}

		res, e := aries2.GetPublicDID(ctx, &aries2.PublicDIDConfig{
			TLSConfig:             tlsConfig,
			OrbDomain:             params.orbClientParameters.domain,
			OrbAnchorOrigin:       params.orbClientParameters.anchorOrigin,
			OrbOperationEndpoints: params.orbClientParameters.operationEndpoints,
			DIDCommEndPoint:       didCommEndpoint,
		})
		if e != nil {
			return fmt.Errorf("creating public DID: %w", e)
		}

		publicDID = res
	}

	router := mux.NewRouter()

	err = addHandlers(params, ctx, router, msgRegistrar, publicDID)
	if err != nil {
		return fmt.Errorf("failed to add handlers: %w", err)
	}

	return serveHubRouter(params, srv, router)
}

func serveHubRouter(params *hubRouterParameters, srv server, router http.Handler) error {
	handler := cors.Default().Handler(router)

	if params.tlsParams.serveCertPath == "" && params.tlsParams.serveKeyPath == "" {
		logger.Infof("starting hub-router server on host:%s", params.hostURL)

		return srv.ListenAndServe(params.hostURL, handler)
	}

	logger.Infof("starting hub-router server on tls host %s", params.hostURL)

	return srv.ListenAndServeTLS(
		params.hostURL,
		params.tlsParams.serveCertPath,
		params.tlsParams.serveKeyPath,
		handler,
	)
}

func addHandlers(params *hubRouterParameters, ctx *context.Provider, router *mux.Router,
	msgRegistrar *msghandler.Registrar, publicDID string) error {
	store, tStore, err := initStores(params.datasourceParams, "", "_txn")
	if err != nil {
		return err
	}

	o, err := operation.New(&operation.Config{
		Aries:          ctx,
		AriesMessenger: ctx.Messenger(),
		MsgRegistrar:   msgRegistrar,
		Storage: &operation.Storage{
			Persistent: store,
			Transient:  tStore,
		},
		PublicDID: publicDID,
	})
	if err != nil {
		return fmt.Errorf("add operation handlers: %w", err)
	}

	kmsHandlers := kms.New(ctx).GetRESTHandlers()

	handlers := o.GetRESTHandlers()

	for _, h := range kmsHandlers {
		handlers = append(handlers, h)
	}

	for _, h := range handlers {
		router.HandleFunc(h.Path(), h.Handle()).Methods(h.Method())
	}

	return nil
}

var (
	//nolint:gochecknoglobals // translation tables copied from afgo for consistency
	keyTypes = map[string]kms2.KeyType{
		"ed25519":           kms2.ED25519Type,
		"ecdsap256ieee1363": kms2.ECDSAP256TypeIEEEP1363,
		"ecdsap256der":      kms2.ECDSAP256TypeDER,
		"ecdsap384ieee1363": kms2.ECDSAP384TypeIEEEP1363,
		"ecdsap384der":      kms2.ECDSAP384TypeDER,
		"ecdsap521ieee1363": kms2.ECDSAP521TypeIEEEP1363,
		"ecdsap521der":      kms2.ECDSAP521TypeDER,
	}

	//nolint:gochecknoglobals // translation tables copied from afgo for consistency
	keyAgreementTypes = map[string]kms2.KeyType{
		"x25519kw": kms2.X25519ECDHKWType,
		"p256kw":   kms2.NISTP256ECDHKWType,
		"p384kw":   kms2.NISTP384ECDHKWType,
		"p521kw":   kms2.NISTP521ECDHKWType,
	}
)

func createAriesAgent( // nolint:funlen // contains all aries initialization
	parameters *hubRouterParameters, tlsConfig *tls.Config, msgRegistrar api.MessageServiceProvider,
) (*aries.Aries, error) {
	store, tStore, err := initStores(parameters.datasourceParams, "_aries", "_ariesps")
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}

	inboundHTTPTransportOpt := defaults.WithInboundHTTPAddr(
		parameters.didCommParameters.httpHostInternal,
		parameters.didCommParameters.httpHostExternal,
		parameters.tlsParams.serveCertPath,
		parameters.tlsParams.serveKeyPath,
	)

	inboundWSTransportOpt := defaults.WithInboundWSAddr(
		parameters.didCommParameters.wsHostInternal,
		parameters.didCommParameters.wsHostExternal,
		parameters.tlsParams.serveCertPath,
		parameters.tlsParams.serveKeyPath,
	)

	outboundHTTP, err := arieshttp.NewOutbound(arieshttp.WithOutboundTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("aries-framework - create outbound tranpsort opts : %w", err)
	}

	outboundWS := ariesws.NewOutbound()

	opts := []aries.Option{
		aries.WithStoreProvider(store),
		aries.WithProtocolStateStoreProvider(tStore),
		inboundHTTPTransportOpt,
		inboundWSTransportOpt,
		aries.WithOutboundTransports(outboundHTTP, outboundWS),
		aries.WithMessageServiceProvider(msgRegistrar),
		aries.WithKeyType(kms2.ECDSAP256TypeIEEEP1363),
		aries.WithKeyAgreementType(kms2.NISTP256ECDHKWType),
	}

	if kt, ok := keyTypes[parameters.didCommParameters.keyType]; ok {
		opts = append(opts, aries.WithKeyType(kt))
	}

	if kat, ok := keyAgreementTypes[parameters.didCommParameters.keyAgreementType]; ok {
		opts = append(opts, aries.WithKeyAgreementType(kat))
	}

	if parameters.didCommParameters.useDIDCommV2 {
		opts = append(opts,
			aries.WithPacker(
				func(provider packer.Provider) (packer.Packer, error) {
					return authcrypt.New(provider, jose.A256CBCHS512)
				},
				func(provider packer.Provider) (packer.Packer, error) {
					return authcrypt.New(provider, jose.A256CBCHS512)
				},
				func(provider packer.Provider) (packer.Packer, error) {
					return anoncrypt.New(provider, jose.A256GCM)
				},
				func(provider packer.Provider) (packer.Packer, error) {
					return legacyauthcrypt.New(provider), nil
				},
			))
	}

	framework, err := aries.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("aries-framework - initialize framework : %w", err)
	}

	return framework, nil
}

func initStores(params *datasourceParams,
	persistentUsagePrefix, transientUsagePrefix string) (persistent, protocolStateStore storage.Provider, err error) {
	persistent, err = initStore(params.persistentURL, storagePrefix+persistentUsagePrefix, params.timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("init persistent storage: %w", err)
	}

	protocolStateStore, err = initStore(params.transientURL, storagePrefix+transientUsagePrefix, params.timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("init protocol state storage: %w", err)
	}

	return persistent, protocolStateStore, nil
}

func initStore(dbURL, prefix string, timeout uint64) (storage.Provider, error) {
	driver, dsn, err := getDBParams(dbURL)
	if err != nil {
		return nil, err
	}

	providerFunc, supported := supportedStorageProviders[driver]
	if !supported {
		return nil, fmt.Errorf("unsupported storage driver: %s", driver)
	}

	var store storage.Provider

	err = retry(func() error {
		var openErr error
		store, openErr = providerFunc(dsn, prefix)

		return openErr
	}, timeout)
	if err != nil {
		return nil, fmt.Errorf("store init - connect to storage at %s : %w", dsn, err)
	}

	logger.Infof("store init - connected to storage at %s", dsn)

	return store, nil
}

func getDBParams(dbURL string) (driver, dsn string, err error) {
	const (
		urlParts = 2
	)

	parsed := strings.SplitN(dbURL, ":", urlParts)

	if len(parsed) != urlParts {
		return "", "", fmt.Errorf("invalid dbURL %s", dbURL)
	}

	driver = parsed[0]

	if driver == databaseTypeMongoDBOption {
		// The MongoDB storage provider needs the full connection string (including the driver as part of it).
		return driver, dbURL, nil
	}

	dsn = strings.TrimPrefix(parsed[1], "//")

	return driver, dsn, nil
}

func retry(fn func() error, timeout uint64) error {
	numRetries := uint64(datasourceTimeoutDefault)

	if timeout != 0 {
		numRetries = timeout
	}

	return backoff.RetryNotify(
		fn,
		backoff.WithMaxRetries(backoff.NewConstantBackOff(sleep), numRetries),
		func(retryErr error, t time.Duration) {
			logger.Warnf(
				"failed to connect to storage, will sleep for %s before trying again : %s\n",
				t, retryErr)
		},
	)
}
