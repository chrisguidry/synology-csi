/*
 * Copyright 2021 Synology Inc.
 */

package webapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/SynologyOpenSource/synology-csi/pkg/logger"
	log "github.com/sirupsen/logrus"
)

// DefaultRequestTimeout bounds one DSM request, so a DSM that stops
// answering fails the CSI call instead of holding it and the sidecar that
// waits on it. It leaves room for the share busy retry to run its whole
// budget and then wait out one request inside the sidecars' RPC timeout.
const DefaultRequestTimeout = 60 * time.Second

type DSM struct {
	Ip         string
	Port       int
	Username   string
	Password   string
	Https      bool
	Controller string
	TLSCACert          string
	TLSServerName      string
	InsecureSkipVerify bool
	SystemInfo

	// One client for each DSM, so requests reuse its connections. A new
	// client for each request costs a TCP connection and a TLS handshake,
	// and one CSI call can send the DSM several requests.
	clientOnce sync.Once
	client     *http.Client
	clientErr  error
	// timeout replaces DefaultRequestTimeout when it is set.
	timeout time.Duration

	// Every request reads the session id, and a re-login writes it, from
	// concurrent CSI calls.
	sessionMu sync.Mutex
	sid       string
	// loginMu lets one re-login run at a time. A DSM session ends for every
	// request at once, and a login for each one would replace the session
	// the one before it just made.
	loginMu sync.Mutex

	// A view from WithContext sends its requests with ctx, and reads the
	// session and the client of root, the DSM it came from.
	ctx  context.Context
	root *DSM
}

// WithContext returns a view of the DSM whose requests carry ctx, so a CSI
// call that its caller cancels or times out stops waiting on the DSM. The
// view shares the DSM's session and connections.
func (dsm *DSM) WithContext(ctx context.Context) *DSM {
	shared := dsm.shared()
	return &DSM{
		Ip:                 shared.Ip,
		Port:               shared.Port,
		Username:           shared.Username,
		Password:           shared.Password,
		Https:              shared.Https,
		Controller:         shared.Controller,
		TLSCACert:          shared.TLSCACert,
		TLSServerName:      shared.TLSServerName,
		InsecureSkipVerify: shared.InsecureSkipVerify,
		SystemInfo:         shared.SystemInfo,
		ctx:                ctx,
		root:               shared,
	}
}

// WithoutCancel returns a view whose requests outlive the cancellation of
// this one's context. A rollback uses it: the call that failed may have
// failed because its context ended, and the resources it made are still on
// the DSM.
func (dsm *DSM) WithoutCancel() *DSM {
	return dsm.WithContext(context.WithoutCancel(dsm.Context()))
}

// session is the session id the DSM's last login returned.
func (dsm *DSM) session() string {
	shared := dsm.shared()
	shared.sessionMu.Lock()
	defer shared.sessionMu.Unlock()
	return shared.sid
}

func (dsm *DSM) setSession(sid string) {
	shared := dsm.shared()
	shared.sessionMu.Lock()
	defer shared.sessionMu.Unlock()
	shared.sid = sid
}

// relogin logs in again unless another request already replaced the
// session that expired.
func (dsm *DSM) relogin(expired string) error {
	shared := dsm.shared()
	shared.loginMu.Lock()
	defer shared.loginMu.Unlock()
	if dsm.session() != expired {
		return nil
	}
	return dsm.Login()
}

// shared is the DSM that holds the session and the client.
func (dsm *DSM) shared() *DSM {
	if dsm.root != nil {
		return dsm.root
	}
	return dsm
}

// Context is the context this view's requests carry.
func (dsm *DSM) Context() context.Context {
	if dsm.ctx != nil {
		return dsm.ctx
	}
	return context.Background()
}

type SystemInfo struct {
	Hostname      string
	SupportNvmeof bool
	FirmwareVer   string
}

type errData struct {
	Code int `json:"code"`
}

type dsmApiResp struct {
	Success bool    `json:"success"`
	Err     errData `json:"error"`
}

type Response struct {
	StatusCode int
	ErrorCode  int
	Success    bool
	Data       interface{}
}

func (dsm *DSM) httpClient() (*http.Client, error) {
	shared := dsm.shared()
	shared.clientOnce.Do(func() {
		shared.client, shared.clientErr = shared.newHTTPClient()
	})
	return shared.client, shared.clientErr
}

func (dsm *DSM) newHTTPClient() (*http.Client, error) {
	timeout := DefaultRequestTimeout
	if dsm.timeout != 0 {
		timeout = dsm.timeout
	}
	if !dsm.Https {
		return &http.Client{Timeout: timeout}, nil
	}

	tlsCfg := &tls.Config{}

	if dsm.TLSServerName != "" {
		tlsCfg.ServerName = dsm.TLSServerName
	}

	if dsm.InsecureSkipVerify {
		if dsm.TLSCACert != "" {
			log.Warnf("tlsCACert is configured but will be ignored for DSM %s because insecureSkipVerify is true.", dsm.Ip)
		}
		log.Warnf("TLS certificate verification is disabled for DSM %s. "+
			"This is insecure; provide tlsCACert instead.", dsm.Ip)
		tlsCfg.InsecureSkipVerify = true
	} else if dsm.TLSCACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			log.Warnf("Failed to load system CA pool for DSM %s, falling back to empty pool: %v", dsm.Ip, err)
			pool = x509.NewCertPool()
		}
		if ok := pool.AppendCertsFromPEM([]byte(dsm.TLSCACert)); !ok {
			return nil, fmt.Errorf("failed to parse TLS CA certificate for DSM %s", dsm.Ip)
		}
		tlsCfg.RootCAs = pool
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg

	return &http.Client{Transport: tr, Timeout: timeout}, nil
}

func (dsm *DSM) sendRequest(data string, apiTemplate interface{}, params url.Values, cgiPath string) (Response, error) {
	sid := dsm.session()
	resp, err := dsm.send(sid, data, apiTemplate, params, cgiPath)
	if err != nil && (resp.ErrorCode == 105 || resp.ErrorCode == 106 || resp.ErrorCode == 119) { // 105: WEBAPI_ERR_NO_PERMISSION, 106: session timeout, 119: WEBAPI_ERR_SID_NOT_FOUND
		// Re-login
		if err := dsm.relogin(sid); err != nil {
			return Response{}, fmt.Errorf("Failed to re-login to DSM: [%s]. err: %v", dsm.Ip, err)
		}
		log.Info("Re-login succeeded.")
		return dsm.send(dsm.session(), data, apiTemplate, params, cgiPath)
	}

	return resp, err
}

func (dsm *DSM) sendRequestWithoutConnectionCheck(data string, apiTemplate interface{}, params url.Values, cgiPath string) (Response, error) {
	return dsm.send(dsm.session(), data, apiTemplate, params, cgiPath)
}

// send makes one request on the session sid.
func (dsm *DSM) send(sid string, data string, apiTemplate interface{}, params url.Values, cgiPath string) (Response, error) {
	client, err := dsm.httpClient()
	if err != nil {
		return Response{}, err
	}

	var req *http.Request
	var cgiUrl string

	// Ex: http://10.12.12.14:5000/webapi/auth.cgi
	if dsm.Https {
		cgiUrl = fmt.Sprintf("https://%s:%d/%s", dsm.Ip, dsm.Port, cgiPath)
	} else {
		cgiUrl = fmt.Sprintf("http://%s:%d/%s", dsm.Ip, dsm.Port, cgiPath)
	}

	baseUrl, err := url.Parse(cgiUrl)
	if err != nil {
		return Response{}, err
	}

	baseUrl.RawQuery = params.Encode()

	if logger.WebapiDebug {
		log.Debugln(baseUrl.RawQuery)
	}

	if data != "" {
		req, err = http.NewRequestWithContext(dsm.Context(), "POST", baseUrl.String(), nil)
	} else {
		req, err = http.NewRequestWithContext(dsm.Context(), "GET", baseUrl.String(), nil)
	}
	if err != nil {
		return Response{}, err
	}

	if sid != "" {
		cookie := http.Cookie{Name: "id", Value: sid}
		req.AddCookie(&cookie)
	}

	// Every request that reaches client.Do is counted once, under the
	// outcome its last return sets.
	outcome := outcomeSuccess
	defer func() { dsm.recordRequest(params.Get("api"), params.Get("method"), outcome) }()

	resp, err := client.Do(req)
	if err != nil {
		outcome = transportOutcome(err)
		return Response{}, err
	}
	// The transport reuses a connection only when the body was read to the
	// end, and the JSON decoder below stops before the trailing newline.
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	// For debug print text body
	var bodyText []byte
	if logger.WebapiDebug {
		bodyText, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			outcome = bodyOutcome(err)
			return Response{}, err
		}
		s := string(bodyText)
		log.Debugln(s)
	}

	if resp.StatusCode != 200 && resp.StatusCode != 302 {
		outcome = outcomeHttpError
		return Response{}, fmt.Errorf("Bad response status code: %d", resp.StatusCode)
	}

	// Strip data json data from response
	type envelop struct {
		dsmApiResp
		Data json.RawMessage `json:"data"`
	}

	e := envelop{}
	var outResp Response

	if logger.WebapiDebug {
		if err := json.Unmarshal(bodyText, &e); err != nil {
			outcome = outcomeBadResponse
			return Response{}, err
		}
	} else {
		decoder := json.NewDecoder(resp.Body)

		if err := decoder.Decode(&e); err != nil {
			outcome = bodyOutcome(err)
			return Response{}, err
		}
	}
	outResp.Success = e.Success
	outResp.ErrorCode = e.Err.Code
	outResp.StatusCode = resp.StatusCode

	if !e.Success {
		outcome = outcomeDsmError
		return outResp, fmt.Errorf("DSM Api error. Error code:%d", outResp.ErrorCode)
	}

	if e.Data != nil {
		if err := json.Unmarshal(e.Data, apiTemplate); err != nil {
			outcome = outcomeBadResponse
			return Response{}, err
		}
	}

	outResp.Data = apiTemplate

	if err != nil {
		return Response{}, err
	}

	return outResp, nil
}

// Login by given user name and password
func (dsm *DSM) Login() error {
	params := url.Values{}
	params.Add("api", "SYNO.API.Auth")
	params.Add("method", "login")
	params.Add("version", "3")
	params.Add("account", dsm.Username)
	params.Add("passwd", dsm.Password)
	params.Add("format", "sid")

	type LoginResp struct {
		Sid string `json:"sid"`
	}

	resp, err := dsm.sendRequestWithoutConnectionCheck("", &LoginResp{}, params, "webapi/auth.cgi")
	if err != nil {
		r, _ := regexp.Compile("passwd=.*&")
		temp := r.ReplaceAllString(err.Error(), "")

		return fmt.Errorf("%s", temp)
	}

	loginResp, ok := resp.Data.(*LoginResp)
	if !ok {
		return fmt.Errorf("Failed to assert response to %T", &LoginResp{})
	}
	dsm.setSession(loginResp.Sid)

	return nil
}

// Logout on current IP and reset the synoToken
func (dsm *DSM) Logout() error {
	params := url.Values{}
	params.Add("api", "SYNO.API.Auth")
	params.Add("method", "logout")
	params.Add("version", "1")

	_, err := dsm.sendRequestWithoutConnectionCheck("", &struct{}{}, params, "webapi/entry.cgi")
	if err != nil {
		return err
	}
	dsm.setSession("")

	return nil
}
