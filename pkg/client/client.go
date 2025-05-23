package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cirruslabs/orchard/internal/config"
	"github.com/cirruslabs/orchard/internal/version"
	"github.com/cirruslabs/orchard/rpc"
	"github.com/coder/websocket"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrFailed       = errors.New("API client failed")
	ErrAPI          = errors.New("API client encountered an API error")
	ErrInvalidState = errors.New("invalid state")
)

type Client struct {
	address            string
	insecure           bool
	trustedCertificate *x509.Certificate
	tlsConfig          *tls.Config

	httpClient *http.Client
	baseURL    *url.URL

	serviceAccountName  string
	serviceAccountToken string

	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

type Config struct {
	Address   string
	TLSConfig *tls.Config
}

func New(opts ...Option) (*Client, error) {
	client := &Client{}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	// Apply defaults
	if client.address == "" {
		if err := client.configureFromDefaultContext(); err != nil {
			return nil, err
		}
	}

	if client.trustedCertificate != nil {
		privatePool := x509.NewCertPool()
		privatePool.AddCert(client.trustedCertificate)

		client.tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    privatePool,
		}

		if len(client.trustedCertificate.DNSNames) != 0 {
			client.tlsConfig.ServerName = client.trustedCertificate.DNSNames[0]
		}
	}

	// Instantiate the HTTP client
	transport := &http.Transport{
		TLSClientConfig: client.tlsConfig,
	}

	if client.dialContext != nil {
		transport.DialContext = client.dialContext
	}

	client.httpClient = &http.Client{
		// The default is zero, which means no timeout, which means that
		// the requests may hang indefinitely. See [1] for more details.
		//
		// [1]: https://github.com/cirruslabs/orchard/issues/152#issuecomment-1927091747
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	url, err := url.Parse(client.address)
	if err != nil {
		return nil, err
	}
	client.baseURL = url

	// Figure out if HTTP (insecure) or HTTPS (secure) was requested,
	// so we can further adapt for gRPC and WebSocket usage patterns
	switch client.baseURL.Scheme {
	case "http":
		client.insecure = true
	case "https":
		// do nothing, we're secure by default
	default:
		return nil, fmt.Errorf("%w: only http https schemes are supported, got %s",
			ErrFailed, client.baseURL.Scheme)
	}

	return client, nil
}

func (client *Client) GRPCTarget() string {
	return client.baseURL.Host
}

func (client *Client) GRPCTransportCredentials() credentials.TransportCredentials {
	if client.insecure {
		return insecure.NewCredentials()
	}

	return credentials.NewTLS(client.tlsConfig)
}

func (client *Client) GPRCMetadata() metadata.MD {
	result := map[string]string{}

	if client.serviceAccountName != "" && client.serviceAccountToken != "" {
		result = map[string]string{
			rpc.MetadataServiceAccountNameKey:  client.serviceAccountName,
			rpc.MetadataServiceAccountTokenKey: client.serviceAccountToken,
		}
	}

	return metadata.New(result)
}

func (client *Client) configureFromDefaultContext() error {
	configHandle, err := config.NewHandle()
	if err != nil {
		return err
	}

	defaultContext, err := configHandle.DefaultContext()
	if err != nil {
		return err
	}

	client.address = defaultContext.URL
	client.serviceAccountName = defaultContext.ServiceAccountName
	client.serviceAccountToken = defaultContext.ServiceAccountToken

	if client.trustedCertificate == nil {
		client.trustedCertificate, err = defaultContext.TrustedCertificate()
		if err != nil {
			return err
		}
	}

	return nil
}

func (client *Client) request(
	ctx context.Context,
	method string,
	path string,
	in interface{},
	out interface{},
	params map[string]string,
) error {
	var body io.Reader

	if in != nil {
		jsonBytes, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("%w to marshal request body: %v", ErrFailed, err)
		}

		body = bytes.NewBuffer(jsonBytes)
	}

	endpointURL := client.formatPath(path)

	values := endpointURL.Query()
	for key, value := range params {
		values.Set(key, value)
	}
	endpointURL.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, method, endpointURL.String(), body)
	if err != nil {
		return fmt.Errorf("%w instantiate a request: %v", ErrFailed, err)
	}

	client.modifyHeader(request.Header)

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w to make a request: %v", ErrFailed, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%w to make a request: %d %s%s",
			ErrAPI, response.StatusCode, http.StatusText(response.StatusCode),
			detailsFromErrorResponseBody(response.Body))
	}

	if out != nil {
		bodyBytes, err := io.ReadAll(response.Body)
		if err != nil {
			return fmt.Errorf("%w to read response body: %v", ErrAPI, err)
		}

		if err := json.Unmarshal(bodyBytes, out); err != nil {
			return fmt.Errorf("%w to unmarshal response body: %v", ErrAPI, err)
		}
	}

	return nil
}

func detailsFromErrorResponseBody(body io.Reader) string {
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return ""
	}

	var errorResponse struct {
		Message string `json:"message"`
	}

	if err := json.Unmarshal(bodyBytes, &errorResponse); err != nil {
		return ""
	}

	if errorResponse.Message != "" {
		return fmt.Sprintf(" (%s)", errorResponse.Message)
	}

	return ""
}

func (client *Client) wsRequest(
	ctx context.Context,
	path string,
	params map[string]string,
) (net.Conn, error) {
	endpointURL := client.formatPath(path)

	// Adapt HTTP scheme to WebSocket scheme
	if client.insecure {
		endpointURL.Scheme = "ws"
	} else {
		endpointURL.Scheme = "wss"
	}

	values := endpointURL.Query()
	for key, value := range params {
		values.Set(key, value)
	}
	endpointURL.RawQuery = values.Encode()

	dialOptions := &websocket.DialOptions{
		HTTPClient: client.httpClient,
		HTTPHeader: make(http.Header),
	}

	client.modifyHeader(dialOptions.HTTPHeader)

	conn, resp, err := websocket.Dial(ctx, endpointURL.String(), dialOptions)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				err = fmt.Errorf("%w (are you sure this VM exists on the controller?)", err)
			}
		}

		return nil, err
	}

	return websocket.NetConn(ctx, conn, websocket.MessageBinary), nil
}

func (client *Client) formatPath(path string) *url.URL {
	endpointURL := &url.URL{
		Scheme: client.baseURL.Scheme,
		User:   client.baseURL.User,
		Host:   client.baseURL.Host,
	}

	return endpointURL.JoinPath("v1", path)
}

func (client *Client) modifyHeader(header http.Header) {
	header.Set("User-Agent", fmt.Sprintf("Orchard/%s", version.FullVersion))

	if client.serviceAccountName != "" && client.serviceAccountToken != "" {
		authPlain := fmt.Sprintf("%s:%s", client.serviceAccountName, client.serviceAccountToken)
		authEncoded := base64.StdEncoding.EncodeToString([]byte(authPlain))
		header.Set("Authorization", fmt.Sprintf("Basic %s", authEncoded))
	}
}

func (client *Client) Check(ctx context.Context) error {
	return client.request(ctx, http.MethodGet, "/", nil, nil, nil)
}

func (client *Client) Workers() *WorkersService {
	return &WorkersService{
		client: client,
	}
}

func (client *Client) VMs() *VMsService {
	return &VMsService{
		client: client,
	}
}

func (client *Client) ServiceAccounts() *ServiceAccountsService {
	return &ServiceAccountsService{
		client: client,
	}
}

func (client *Client) Controller() *ControllerService {
	return &ControllerService{
		client: client,
	}
}

func (client *Client) ClusterSettings() *ClusterSettingsService {
	return &ClusterSettingsService{
		client: client,
	}
}

func (client *Client) RPC() *RPCService {
	return &RPCService{
		client: client,
	}
}
