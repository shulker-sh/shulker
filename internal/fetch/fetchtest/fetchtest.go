// Package fetchtest lets tests serve shulker's downloads over TLS, since fetch refuses plain http.
package fetchtest

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
)

// TrustTestServers has http.DefaultTransport, the one a fetch.New client uses, accept a TLS test
// server's certificate. A package whose tests fetch calls it from an init.
func TrustTestServers() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}

// Routed is a client that sends a request for any of hosts to srv, so a test can name the real
// hosts a check allows.
func Routed(srv *httptest.Server, hosts ...string) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	var d net.Dialer
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil && slices.Contains(hosts, host) {
			addr = srv.Listener.Addr().String()
		}
		return d.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: t}
}
