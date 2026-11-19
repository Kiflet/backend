// Package garage sets up a Garage cluster (https://garagehq.deuxfleurs.fr/)
// for Kiflet through its admin API (v2): the cluster layout, the access
// keys, the buckets, which key may do what in each, and their CORS and
// lifecycle rules. Each step is skipped when it is done already, so the
// setup runs at every deploy.
//
// The app itself talks to Garage only through the S3 API (pkg/storage);
// this package is for the one-off `kiflet setup-storage` job.
package garage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Plan is what the cluster should hold.
type Plan struct {
	// Nodes is how many storage nodes the layout waits for before it is
	// made; a layout already made is left as it is (changing a running
	// cluster's layout is an operator's decision).
	Nodes int
	Keys  []Key
	// Buckets are made if missing; their grants, CORS and lifecycle rules
	// are set every time.
	Buckets []Bucket
}

// Key is an access key, imported with the ID and secret given so the
// app's settings can name them before the cluster exists.
type Key struct {
	ID, Secret, Name string
}

// Bucket is a bucket and its rules.
type Bucket struct {
	Name   string
	Grants []Grant
	// CORSOrigins may PUT and GET with a presigned URL from a browser.
	CORSOrigins []string
	// ExpireDays deletes objects that old; 0 keeps them.
	ExpireDays int
	// AbortUploadsDays removes multipart uploads left unfinished that long.
	AbortUploadsDays int
}

// Grant is what a key may do in a bucket.
type Grant struct {
	KeyID              string
	Read, Write, Owner bool
}

var (
	keyIDPattern  = regexp.MustCompile(`^GK[0-9a-f]{24}$`)
	secretPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// What Garage and S3 accept as a bucket name.
	bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// Validate says what is wrong with a plan before anything is changed.
func (p Plan) Validate() error {
	var errs []error
	if p.Nodes < 1 {
		errs = append(errs, errors.New("at least one node"))
	}
	keys := map[string]bool{}
	for _, k := range p.Keys {
		if !keyIDPattern.MatchString(k.ID) {
			errs = append(errs, fmt.Errorf("access key %q: Garage's are GK and 24 hexadecimal characters (GK$(openssl rand -hex 12))", k.ID))
		}
		if !secretPattern.MatchString(k.Secret) {
			errs = append(errs, fmt.Errorf("the secret of %q: Garage's are 64 hexadecimal characters (openssl rand -hex 32)", k.ID))
		}
		keys[k.ID] = true
	}
	for _, b := range p.Buckets {
		if !bucketPattern.MatchString(b.Name) {
			errs = append(errs, fmt.Errorf("bucket %q: lower-case letters, digits, dots and hyphens, 3 to 63", b.Name))
		}
		for _, g := range b.Grants {
			if !keys[g.KeyID] {
				errs = append(errs, fmt.Errorf("bucket %q grants a key the plan doesn't have, %q", b.Name, g.KeyID))
			}
		}
	}
	return errors.Join(errs...)
}

// Client calls a Garage node's admin API.
type Client struct {
	URL   string // e.g. http://garage:3903
	Token string
	HTTP  *http.Client
	// Log reports each step; nil logs nothing.
	Log func(format string, args ...any)
}

var (
	// ErrNotFound is the admin API's 404.
	ErrNotFound = errors.New("not found")
	// ErrUnauthorized is its 401 and 403: the token is wrong, and waiting
	// won't change that.
	ErrUnauthorized = errors.New("the admin token was refused")
)

func (c *Client) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	u := strings.TrimRight(c.URL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%s %s: %w", method, path, ErrNotFound)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s %s: %w", method, path, ErrUnauthorized)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
	}
	return nil
}

type node struct {
	ID            string  `json:"id"`
	Hostname      *string `json:"hostname"`
	IsUp          bool    `json:"isUp"`
	DataPartition *struct {
		Total int64 `json:"total"`
	} `json:"dataPartition"`
}

type clusterStatus struct {
	LayoutVersion int64  `json:"layoutVersion"`
	Nodes         []node `json:"nodes"`
}

type layout struct {
	Version int64 `json:"version"`
	Roles   []struct {
		ID string `json:"id"`
	} `json:"roles"`
}

type roleChange struct {
	ID       string   `json:"id"`
	Zone     string   `json:"zone"`
	Capacity int64    `json:"capacity"`
	Tags     []string `json:"tags"`
}

// Healthy says whether the cluster can serve reads and writes: the admin
// API's /health, which needs no token.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/health", nil)
	if err != nil {
		return false
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Apply makes the cluster match the plan, waiting up to wait for the nodes
// to come up and the cluster to be healthy.
func (c *Client) Apply(ctx context.Context, p Plan, wait time.Duration) error {
	if err := p.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if err := c.ensureLayout(ctx, p.Nodes); err != nil {
		return fmt.Errorf("layout: %w", err)
	}
	for !c.Healthy(ctx) {
		if err := sleep(ctx, time.Second); err != nil {
			return errors.New("the cluster isn't healthy: /health doesn't answer 200")
		}
	}
	for _, k := range p.Keys {
		if err := c.ensureKey(ctx, k); err != nil {
			return fmt.Errorf("key %s: %w", k.ID, err)
		}
	}
	for _, b := range p.Buckets {
		if err := c.ensureBucket(ctx, b); err != nil {
			return fmt.Errorf("bucket %s: %w", b.Name, err)
		}
	}
	return nil
}

// ensureLayout gives every node a role, each in a zone of its own, once
// the expected number are up. A layout with roles already is left alone.
func (c *Client) ensureLayout(ctx context.Context, want int) error {
	for {
		var l layout
		if err := c.call(ctx, http.MethodGet, "/v2/GetClusterLayout", nil, nil, &l); err != nil {
			if ctx.Err() != nil || errors.Is(err, ErrUnauthorized) {
				return err
			}
			c.logf("waiting for Garage's admin API: %v", err)
		} else if len(l.Roles) > 0 {
			c.logf("layout version %d has %d node(s): left as it is", l.Version, len(l.Roles))
			return nil
		} else {
			var st clusterStatus
			if err := c.call(ctx, http.MethodGet, "/v2/GetClusterStatus", nil, nil, &st); err != nil {
				return err
			}
			var up []node
			for _, n := range st.Nodes {
				if n.IsUp {
					up = append(up, n)
				}
			}
			if len(up) >= want {
				return c.makeLayout(ctx, l.Version, up)
			}
			c.logf("%d of %d node(s) up; waiting", len(up), want)
		}
		if err := sleep(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

func (c *Client) makeLayout(ctx context.Context, version int64, nodes []node) error {
	var roles []roleChange
	for i, n := range nodes {
		zone := fmt.Sprintf("zone%d", i+1)
		if n.Hostname != nil && *n.Hostname != "" {
			zone = *n.Hostname
		}
		// The disk's size: Garage spreads data in proportion to it.
		capacity := int64(1) << 40
		if n.DataPartition != nil && n.DataPartition.Total > 0 {
			capacity = n.DataPartition.Total
		}
		roles = append(roles, roleChange{ID: n.ID, Zone: zone, Capacity: capacity, Tags: []string{}})
	}
	if err := c.call(ctx, http.MethodPost, "/v2/UpdateClusterLayout", nil, map[string]any{"roles": roles}, nil); err != nil {
		return err
	}
	if err := c.call(ctx, http.MethodPost, "/v2/ApplyClusterLayout", nil, map[string]any{"version": version + 1}, nil); err != nil {
		return err
	}
	c.logf("layout version %d: %d node(s)", version+1, len(roles))
	return nil
}

func (c *Client) ensureKey(ctx context.Context, k Key) error {
	err := c.call(ctx, http.MethodGet, "/v2/GetKeyInfo", url.Values{"id": {k.ID}}, nil, nil)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	name := k.Name
	if name == "" {
		name = k.ID
	}
	if err := c.call(ctx, http.MethodPost, "/v2/ImportKey", nil,
		map[string]any{"accessKeyId": k.ID, "secretAccessKey": k.Secret, "name": name}, nil); err != nil {
		return err
	}
	c.logf("key %s (%s) imported", k.ID, name)
	return nil
}

func (c *Client) ensureBucket(ctx context.Context, b Bucket) error {
	var info struct {
		ID string `json:"id"`
	}
	err := c.call(ctx, http.MethodGet, "/v2/GetBucketInfo", url.Values{"globalAlias": {b.Name}}, nil, &info)
	if errors.Is(err, ErrNotFound) {
		err = c.call(ctx, http.MethodPost, "/v2/CreateBucket", nil, map[string]any{"globalAlias": b.Name}, &info)
		if err == nil {
			c.logf("bucket %s made", b.Name)
		}
	}
	if err != nil {
		return err
	}
	for _, g := range b.Grants {
		if err := c.call(ctx, http.MethodPost, "/v2/AllowBucketKey", nil, map[string]any{
			"bucketId": info.ID, "accessKeyId": g.KeyID,
			"permissions": map[string]bool{"read": g.Read, "write": g.Write, "owner": g.Owner},
		}, nil); err != nil {
			return err
		}
	}
	// The rules are replaced whole, so a rule taken out of the plan goes.
	cors := []map[string]any{}
	if len(b.CORSOrigins) > 0 {
		cors = append(cors, map[string]any{
			"AllowedOrigin": b.CORSOrigins,
			"AllowedMethod": []string{"GET", "HEAD", "PUT"},
			"AllowedHeader": []string{"*"},
			"ExposeHeader":  []string{"ETag"},
			"MaxAgeSeconds": 3600,
		})
	}
	lifecycle := []map[string]any{}
	if b.ExpireDays > 0 {
		lifecycle = append(lifecycle, map[string]any{
			"ID": "expire", "Status": "Enabled", "Filter": map[string]any{"Prefix": ""},
			"Expiration": map[string]any{"Days": b.ExpireDays},
		})
	}
	if b.AbortUploadsDays > 0 {
		lifecycle = append(lifecycle, map[string]any{
			"ID": "abort-uploads", "Status": "Enabled", "Filter": map[string]any{"Prefix": ""},
			"AbortIncompleteMultipartUpload": map[string]any{"DaysAfterInitiation": b.AbortUploadsDays},
		})
	}
	return c.call(ctx, http.MethodPost, "/v2/UpdateBucket", url.Values{"id": {info.ID}},
		map[string]any{"corsRules": cors, "lifecycleRules": lifecycle}, nil)
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
