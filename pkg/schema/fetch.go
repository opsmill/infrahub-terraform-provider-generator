package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpClient bounds every schema fetch with a timeout so a hung or half-open
// Infrahub cannot stall generation, independent of the caller's context.
//
// CheckRedirect drops the X-INFRAHUB-KEY header when a redirect crosses to a
// different host: Go's client only strips Authorization/Cookie/WWW-Authenticate
// on a cross-host redirect, not arbitrary custom headers, so without this a 30x
// to another host would re-send the API token to that host. The comparison is
// on the hostname only (not the port or scheme), so a same-host http->https
// upgrade or port change keeps the token while a true cross-host redirect drops
// it.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.URL.Hostname() != via[0].URL.Hostname() {
			req.Header.Del("X-INFRAHUB-KEY")
		}
		return nil
	},
}

// apiSchema mirrors the subset of the /api/schema response we consume. The
// endpoint inlines inherited (generic) attributes into each node's attributes
// list, so generics need not be merged here.
type apiSchema struct {
	Nodes []struct {
		Kind       string `json:"kind"`
		Attributes []struct {
			Name     string `json:"name"`
			Kind     string `json:"kind"`
			Optional bool   `json:"optional"`
		} `json:"attributes"`
	} `json:"nodes"`
}

// Fetch reads the schema for branch from an Infrahub instance at address,
// authenticating with token via the X-INFRAHUB-KEY header, and builds a
// Registry. It returns an error on any transport, status, or decode failure so
// callers never silently generate mistyped code.
func Fetch(ctx context.Context, address, token, branch string) (*Registry, error) {
	endpoint := strings.TrimRight(address, "/") + "/api/schema?" + url.Values{"branch": {branch}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building schema request: %w", err)
	}
	req.Header.Set("X-INFRAHUB-KEY", token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching schema from %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading schema response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("schema endpoint %s returned HTTP %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed apiSchema
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decoding schema response: %w", err)
	}

	reg := &Registry{nodes: make(map[string]map[string]Attribute, len(parsed.Nodes))}
	for _, n := range parsed.Nodes {
		attrs := make(map[string]Attribute, len(n.Attributes))
		for _, a := range n.Attributes {
			attrs[a.Name] = Attribute{Kind: a.Kind, Optional: a.Optional}
		}
		reg.nodes[n.Kind] = attrs
	}
	return reg, nil
}
