package services

import (
	"net/http"
	"net/url"
)

// Capture every HTTP request before the transport can reuse an HTTP/2 tunnel.
// The fixed proxy belongs to this transport, rather than to a later config
// snapshot, so both the actual connection and its trace use the same route.
type upstreamProxyTransport struct {
	transport *http.Transport
	proxy     *url.URL
}

func (t *upstreamProxyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	recordNetworkRoute(r, t.proxy)
	response, err := t.transport.RoundTrip(r)
	if response != nil {
		if trace, ok := r.Context().Value(networkHTTPTraceKey).(*upstreamRequestTrace); ok {
			trace.recordResponseHeaders(response.Header)
		}
	}
	return response, err
}

func (t *upstreamProxyTransport) CloseIdleConnections() {
	t.transport.CloseIdleConnections()
}
