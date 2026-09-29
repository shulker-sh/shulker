// Package fetchtest lets tests serve shulker's downloads over TLS, since fetch refuses plain http.
package fetchtest

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
)

// TrustTestServers has http.DefaultTransport, the one a fetch.New client uses, and git accept a
// TLS test server's certificate. A package whose tests fetch calls it from an init.
func TrustTestServers() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	os.Setenv("GIT_SSL_NO_VERIFY", "true")
}

// Routed is a client that sends a request for any of hosts to srv, so a test can name the real
// hosts a check allows. A host starting with a dot stands for every host ending in it.
func Routed(srv *httptest.Server, hosts ...string) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	var d net.Dialer
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil && routes(hosts, host) {
			addr = srv.Listener.Addr().String()
		}
		return d.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: t}
}

func routes(hosts []string, host string) bool {
	return slices.ContainsFunc(hosts, func(h string) bool {
		return h == host || strings.HasPrefix(h, ".") && strings.HasSuffix(host, h)
	})
}
