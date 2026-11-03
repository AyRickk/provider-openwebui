// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/AyRickk/provider-openwebui/apis/v1alpha1"
	"github.com/AyRickk/provider-openwebui/internal/client"
	"github.com/AyRickk/provider-openwebui/internal/client/transport"
)

// A Connection is a checked connection to an Open WebUI instance: its version is detected,
// its profile selected and the API key accepted.
type Connection struct {
	// Doer sends requests to the instance.
	Doer client.Doer

	// Version is the version the instance reports.
	Version client.Version

	// Profile is the API profile of Version's minor, or the newest profile when
	// Unsupported.
	Profile *client.Profile

	// Unsupported reports that Version's minor has no profile, and that
	// allowUnsupportedVersions lets the provider use the newest one.
	Unsupported bool
}

// A NewDoerFn returns the Doer that sends requests to the instance cfg configures.
type NewDoerFn func(cfg transport.Config) (client.Doer, error)

// A Connector connects to the Open WebUI instance of a provider config. One Connector is
// shared by the provider config controllers and every resource controller, so that they
// share the version cache. It is safe for concurrent use.
type Connector struct {
	kube     kclient.Client
	usage    resource.ModernTracker
	versions *client.VersionCache
	newDoer  NewDoerFn
	metrics  *transport.Metrics
	log      logging.Logger

	mu sync.Mutex
	// doers holds the Doer of each provider config, by id, so that its connections are
	// reused and its warnings logged once.
	doers map[string]cachedDoer
}

// A cachedDoer is a Doer and the fingerprint of the transport configuration it was built
// with.
type cachedDoer struct {
	fingerprint [sha256.Size]byte
	doer        client.Doer
}

// A ConnectorOption configures a Connector.
type ConnectorOption func(*Connector)

// WithNewDoer replaces how the Connector builds a Doer, by default with transport.New.
func WithNewDoer(fn NewDoerFn) ConnectorOption {
	return func(c *Connector) { c.newDoer = fn }
}

// WithUsageTracker replaces how the Connector tracks that a managed resource uses its
// provider config, by default with a ProviderConfigUsage.
func WithUsageTracker(t resource.ModernTracker) ConnectorOption {
	return func(c *Connector) { c.usage = t }
}

// WithMetrics sets the request counter of the Doers the Connector builds.
func WithMetrics(m *transport.Metrics) ConnectorOption {
	return func(c *Connector) { c.metrics = m }
}

// WithLogger sets the logger of the Doers the Connector builds.
func WithLogger(l logging.Logger) ConnectorOption {
	return func(c *Connector) { c.log = l }
}

// NewConnector returns a Connector that reads provider configs and their Secrets with kube.
func NewConnector(kube kclient.Client, o ...ConnectorOption) *Connector {
	c := &Connector{
		kube:     kube,
		usage:    resource.NewProviderConfigUsageTracker(kube, &v1alpha1.ProviderConfigUsage{}),
		versions: client.NewVersionCache(client.DefaultVersionTTL),
		newDoer:  func(cfg transport.Config) (client.Doer, error) { return transport.New(cfg) },
		log:      logging.NewNopLogger(),
		doers:    map[string]cachedDoer{},
	}
	for _, fn := range o {
		fn(c)
	}
	return c
}

// secretKey selects a key of a Secret.
type secretKey struct {
	namespace, name, key string
}

// settings are what the Connector needs of a provider config, whatever its kind.
type settings struct {
	// id identifies the provider config, e.g. ProviderConfig/team/owui.
	id string
	v1alpha1.ConnectionSettings
	source v1alpha1.CredentialsSource
	apiKey secretKey
	caCert *secretKey
}

// settingsOf returns the settings of pc, a ProviderConfig or a ClusterProviderConfig.
func settingsOf(pc resource.ProviderConfig) (settings, error) {
	switch pc := pc.(type) {
	case *v1alpha1.ProviderConfig:
		key := pc.Spec.Credentials.SecretRef
		s := settings{
			id:                 v1alpha1.ProviderConfigKind + "/" + pc.GetNamespace() + "/" + pc.GetName(),
			ConnectionSettings: pc.Spec.ConnectionSettings,
			source:             pc.Spec.Credentials.Source,
			apiKey:             secretKey{namespace: pc.GetNamespace(), name: key.Name, key: key.Key},
		}
		if ref := pc.Spec.CACertSecretRef; ref != nil {
			s.caCert = &secretKey{namespace: pc.GetNamespace(), name: ref.Name, key: ref.Key}
		}
		return s, nil
	case *v1alpha1.ClusterProviderConfig:
		key := pc.Spec.Credentials.SecretRef
		s := settings{
			id:                 v1alpha1.ClusterProviderConfigKind + "/" + pc.GetName(),
			ConnectionSettings: pc.Spec.ConnectionSettings,
			source:             pc.Spec.Credentials.Source,
			apiKey:             secretKey{namespace: key.Namespace, name: key.Name, key: key.Key},
		}
		if ref := pc.Spec.CACertSecretRef; ref != nil {
			s.caCert = &secretKey{namespace: ref.Namespace, name: ref.Name, key: ref.Key}
		}
		return s, nil
	}
	return settings{}, errors.Errorf("unsupported provider config type %T", pc)
}

// Connect connects to the instance of the provider config mg references, after tracking
// that mg uses it. A ProviderConfig is looked up in mg's namespace.
func (c *Connector) Connect(ctx context.Context, mg resource.ModernManaged) (*Connection, error) {
	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, "cannot track the provider config usage")
	}
	ref := mg.GetProviderConfigReference()
	var pc resource.ProviderConfig
	key := types.NamespacedName{Name: ref.Name}
	switch ref.Kind {
	case v1alpha1.ProviderConfigKind:
		pc = &v1alpha1.ProviderConfig{}
		key.Namespace = mg.GetNamespace()
	case v1alpha1.ClusterProviderConfigKind:
		pc = &v1alpha1.ClusterProviderConfig{}
	default:
		return nil, errors.Errorf("unsupported provider config kind %q", ref.Kind)
	}
	if err := c.kube.Get(ctx, key, pc); err != nil {
		return nil, errors.Wrapf(err, "cannot get %s %q", ref.Kind, ref.Name)
	}
	return c.ConnectConfig(ctx, pc)
}

// ConnectConfig connects to the instance of pc, a ProviderConfig or a
// ClusterProviderConfig: it reads the API key, detects the version, selects the profile and
// checks the credentials.
func (c *Connector) ConnectConfig(ctx context.Context, pc resource.ProviderConfig) (*Connection, error) {
	s, err := settingsOf(pc)
	if err != nil {
		return nil, err
	}
	token, err := c.apiKey(ctx, s)
	if err != nil {
		return nil, errors.Wrap(err, "cannot read the API key")
	}
	var caCert []byte
	if s.caCert != nil {
		if caCert, err = c.secretValue(ctx, *s.caCert); err != nil {
			return nil, errors.Wrap(err, "cannot read the CA certificate")
		}
	}
	d, err := c.doer(s.id, transport.Config{
		BaseURL: s.URL,
		// A Secret made from a file often ends with a newline, which is not part of the key.
		Token:              strings.TrimSpace(string(token)),
		CACertPEM:          caCert,
		AuthHeader:         s.AuthHeader,
		InsecureSkipVerify: s.InsecureSkipVerify,
		Metrics:            c.metrics,
		Logger:             c.log.WithValues("providerConfig", s.id),
	})
	if err != nil {
		return nil, errors.Wrap(err, "cannot build the Open WebUI client")
	}
	// The URL is part of the key, so that a changed URL is detected again.
	v, err := c.versions.Get(ctx, s.id+"@"+s.URL, d)
	if err != nil {
		return nil, err
	}
	p, unsupported, err := client.SelectProfile(v, s.AllowUnsupportedVersions)
	if err != nil {
		return nil, err
	}
	if err := p.CheckCredentials(ctx, d); err != nil {
		return nil, err
	}
	return &Connection{Doer: d, Version: v, Profile: p, Unsupported: unsupported}, nil
}

// doer returns the Doer of the provider config id for cfg, built again only when cfg
// changed. The fingerprint hashes cfg, so that the cache never holds the token in clear
// beside the Doer.
func (c *Connector) doer(id string, cfg transport.Config) (client.Doer, error) {
	h := sha256.New()
	for _, v := range []string{cfg.BaseURL, cfg.Token, cfg.AuthHeader, string(cfg.CACertPEM), strconv.FormatBool(cfg.InsecureSkipVerify)} {
		// Each value is length-prefixed, so that no two configurations hash alike.
		_, _ = fmt.Fprintf(h, "%d:%s", len(v), v)
	}
	var fp [sha256.Size]byte
	copy(fp[:], h.Sum(nil))

	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.doers[id]; ok && e.fingerprint == fp {
		return e.doer, nil
	}
	d, err := c.newDoer(cfg)
	if err != nil {
		return nil, err
	}
	c.doers[id] = cachedDoer{fingerprint: fp, doer: d}
	return d, nil
}

// apiKey returns the API key of s, from its credentials source.
func (c *Connector) apiKey(ctx context.Context, s settings) ([]byte, error) {
	if s.source != v1alpha1.CredentialsSourceSecret {
		return nil, errors.Errorf("unsupported credentials source %q", s.source)
	}
	return c.secretValue(ctx, s.apiKey)
}

// secretValue returns the value of a Secret key. Errors name the Secret and the key, never
// the value.
func (c *Connector) secretValue(ctx context.Context, sk secretKey) ([]byte, error) {
	s := &corev1.Secret{}
	if err := c.kube.Get(ctx, types.NamespacedName{Namespace: sk.namespace, Name: sk.name}, s); err != nil {
		return nil, errors.Wrapf(err, "cannot get Secret %q", sk.namespace+"/"+sk.name)
	}
	v, ok := s.Data[sk.key]
	if !ok {
		return nil, errors.Errorf("Secret %q has no key %q", sk.namespace+"/"+sk.name, sk.key)
	}
	return v, nil
}
