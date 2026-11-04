// SPDX-FileCopyrightText: 2025 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	kfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/AyRickk/provider-openwebui/apis/v1alpha1"
	"github.com/AyRickk/provider-openwebui/internal/client"
	"github.com/AyRickk/provider-openwebui/internal/client/transport"
)

const (
	// apiKey is the API key the test Secrets hold. No status, event or error may contain it.
	apiKey = "sk-0123456789abcdef0123456789abcdef"
	// tokenKey is the Secret key that holds the API key.
	tokenKey = "token"

	v0114    = "0.11.4"
	v0124    = "0.12.4"
	v0130    = "0.13.0"
	minor011 = "0.11"
	minor012 = "0.12"
)

// fakeOpenWebUI stands in for an Open WebUI instance, behind the Doer the connector builds.
type fakeOpenWebUI struct {
	// version is answered on GET /api/version.
	version string
	// credentials is answered on the credential check.
	credentials error

	// cfg is the transport configuration the connector built the Doer with.
	cfg transport.Config
}

func (f *fakeOpenWebUI) newDoer(cfg transport.Config) (client.Doer, error) {
	f.cfg = cfg
	return f, nil
}

func (f *fakeOpenWebUI) Do(_ context.Context, req transport.Request, out any) error {
	switch req.String() {
	case http.MethodGet + " /api/version":
		return json.Unmarshal([]byte(`{"version":"`+f.version+`","deployment_id":"d"}`), out)
	case http.MethodGet + " /api/v1/users/default/permissions":
		return f.credentials
	}
	return errors.Errorf("unexpected request %s", req)
}

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.SchemeBuilder.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func kube(t *testing.T, objs ...kclient.Object) kclient.Client {
	t.Helper()
	return kfake.NewClientBuilder().
		WithScheme(scheme(t)).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.ProviderConfig{}, &v1alpha1.ClusterProviderConfig{}).
		Build()
}

func secret(namespace, name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

func providerConfig(mod ...func(*v1alpha1.ProviderConfig)) *v1alpha1.ProviderConfig {
	pc := &v1alpha1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "owui"},
		Spec: v1alpha1.ProviderConfigSpec{
			ConnectionSettings: v1alpha1.ConnectionSettings{URL: "https://owui.example.com"},
			Credentials: v1alpha1.ProviderCredentials{
				Source:    v1alpha1.CredentialsSourceSecret,
				SecretRef: v1alpha1.LocalSecretKeySelector{Name: "owui-key", Key: tokenKey},
			},
		},
	}
	for _, m := range mod {
		m(pc)
	}
	return pc
}

func clusterProviderConfig(mod ...func(*v1alpha1.ClusterProviderConfig)) *v1alpha1.ClusterProviderConfig {
	pc := &v1alpha1.ClusterProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v1alpha1.ClusterProviderConfigSpec{
			ConnectionSettings: v1alpha1.ConnectionSettings{URL: "https://owui.example.com"},
			Credentials: v1alpha1.ClusterProviderCredentials{
				Source:    v1alpha1.CredentialsSourceSecret,
				SecretRef: v1alpha1.SecretKeySelector{Namespace: "crossplane-system", Name: "owui-key", Key: tokenKey},
			},
		},
	}
	for _, m := range mod {
		m(pc)
	}
	return pc
}

// connection is the comparable part of a Connection.
type connection struct {
	Version     string
	Profile     string
	Unsupported bool
}

func summarize(c *Connection) *connection {
	if c == nil {
		return nil
	}
	return &connection{Version: c.Version.String(), Profile: c.Profile.Minor(), Unsupported: c.Unsupported}
}

func TestConnectConfig(t *testing.T) {
	type want struct {
		conn *connection
		cfg  *transport.Config
		// err is the sentinel the error matches, if any.
		err error
		// msg is part of the error message, if any.
		msg string
	}
	cases := map[string]struct {
		reason string
		owui   fakeOpenWebUI
		objs   []kclient.Object
		pc     resource.ProviderConfig
		want   want
	}{
		"ProviderConfig": {
			reason: "A ProviderConfig connects with the API key of the Secret in its namespace.",
			owui:   fakeOpenWebUI{version: v0124},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc:     providerConfig(),
			want:   want{conn: &connection{Version: v0124, Profile: minor012}},
		},
		"ClusterProviderConfig": {
			reason: "A ClusterProviderConfig reads its Secrets in the namespace they name, and passes its settings to the transport.",
			owui:   fakeOpenWebUI{version: v0114},
			objs: []kclient.Object{
				secret("crossplane-system", "owui-key", map[string]string{tokenKey: apiKey + "\n"}),
				secret("crossplane-system", "owui-ca", map[string]string{"ca.crt": "PEM"}),
			},
			pc: clusterProviderConfig(func(pc *v1alpha1.ClusterProviderConfig) {
				pc.Spec.AuthHeader = "x-api-key"
				pc.Spec.InsecureSkipVerify = true
				pc.Spec.CACertSecretRef = &v1alpha1.SecretKeySelector{Namespace: "crossplane-system", Name: "owui-ca", Key: "ca.crt"}
			}),
			want: want{
				conn: &connection{Version: v0114, Profile: minor011},
				cfg: &transport.Config{
					BaseURL:            "https://owui.example.com",
					Token:              apiKey,
					AuthHeader:         "x-api-key",
					CACertPEM:          []byte("PEM"),
					InsecureSkipVerify: true,
				},
			},
		},
		"UnknownSource": {
			reason: "A credentials source other than Secret is an error naming it.",
			owui:   fakeOpenWebUI{version: v0124},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc: providerConfig(func(pc *v1alpha1.ProviderConfig) {
				pc.Spec.Credentials.Source = "Environment"
			}),
			want: want{msg: `cannot read the API key: unsupported credentials source "Environment"`},
		},
		"MissingSecret": {
			reason: "A missing API key Secret is an error naming it.",
			owui:   fakeOpenWebUI{version: v0124},
			pc:     providerConfig(),
			want:   want{msg: `cannot read the API key: cannot get Secret "team/owui-key"`},
		},
		"MissingKey": {
			reason: "A Secret without the selected key is an error naming both.",
			owui:   fakeOpenWebUI{version: v0124},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{"other": apiKey})},
			pc:     providerConfig(),
			want:   want{msg: `cannot read the API key: Secret "team/owui-key" has no key "token"`},
		},
		"MissingCACert": {
			reason: "A missing CA certificate Secret is an error naming it.",
			owui:   fakeOpenWebUI{version: v0124},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc: providerConfig(func(pc *v1alpha1.ProviderConfig) {
				pc.Spec.CACertSecretRef = &v1alpha1.LocalSecretKeySelector{Name: "owui-ca", Key: "ca.crt"}
			}),
			want: want{msg: `cannot read the CA certificate: cannot get Secret "team/owui-ca"`},
		},
		"BadKey": {
			reason: "An API key Open WebUI rejects is unauthorized.",
			owui:   fakeOpenWebUI{version: v0124, credentials: errors.Wrap(transport.ErrUnauthorized, "GET /api/v1/users/default/permissions: 401")},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc:     providerConfig(),
			want:   want{err: transport.ErrUnauthorized},
		},
		"UnknownMinor": {
			reason: "A minor without a profile is unsupported, with the supported range in the message.",
			owui:   fakeOpenWebUI{version: v0130},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc:     providerConfig(),
			want:   want{err: client.ErrUnsupportedVersion, msg: "Open WebUI v0.13.0 is not in the supported range [0.11, 0.12]"},
		},
		"UnknownMinorAllowed": {
			reason: "With allowUnsupportedVersions, a minor without a profile uses the newest profile and is reported unsupported.",
			owui:   fakeOpenWebUI{version: v0130},
			objs:   []kclient.Object{secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			pc: providerConfig(func(pc *v1alpha1.ProviderConfig) {
				pc.Spec.AllowUnsupportedVersions = true
			}),
			want: want{conn: &connection{Version: v0130, Profile: minor012, Unsupported: true}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewConnector(kube(t, tc.objs...), WithNewDoer(tc.owui.newDoer))
			got, err := c.ConnectConfig(context.Background(), tc.pc)
			checkError(t, tc.reason, "ConnectConfig(...)", err, tc.want.err, tc.want.msg)
			if diff := cmp.Diff(tc.want.conn, summarize(got)); diff != "" {
				t.Errorf("\n%s\nConnectConfig(...): -want, +got:\n%s", tc.reason, diff)
			}
			if tc.want.cfg != nil {
				if diff := cmp.Diff(*tc.want.cfg, tc.owui.cfg, cmpopts.IgnoreFields(transport.Config{}, "Metrics", "Logger")); diff != "" {
					t.Errorf("\n%s\nConnectConfig(...): transport config: -want, +got:\n%s", tc.reason, diff)
				}
			}
		})
	}
}

// checkError checks that err matches the sentinel want and contains msg, or is nil when both
// are empty, and that it never contains the API key.
func checkError(t *testing.T, reason, call string, err, want error, msg string) {
	t.Helper()
	if want == nil && msg == "" {
		if err != nil {
			t.Fatalf("\n%s\n%s: unexpected error: %v", reason, call, err)
		}
		return
	}
	if err == nil {
		t.Fatalf("\n%s\n%s: want error, got none", reason, call)
	}
	if want != nil && !errors.Is(err, want) {
		t.Errorf("\n%s\n%s: error %q does not match %q", reason, call, err, want)
	}
	if !strings.Contains(err.Error(), msg) {
		t.Errorf("\n%s\n%s: error %q does not contain %q", reason, call, err, msg)
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Errorf("\n%s\n%s: error %q contains the API key", reason, call, err)
	}
}

func managed(kind, name string) *fake.ModernManaged {
	mg := &fake.ModernManaged{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "admins"}}
	mg.SetProviderConfigReference(&xpv2.ProviderConfigReference{Kind: kind, Name: name})
	return mg
}

func TestConnect(t *testing.T) {
	errBoom := errors.New("boom")
	type want struct {
		conn    *connection
		tracked bool
		err     error
		msg     string
	}
	cases := map[string]struct {
		reason string
		track  error
		objs   []kclient.Object
		mg     resource.ModernManaged
		want   want
	}{
		"ProviderConfig": {
			reason: "A managed resource uses the ProviderConfig of its namespace, and its usage is tracked.",
			objs:   []kclient.Object{providerConfig(), secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			mg:     managed(v1alpha1.ProviderConfigKind, "owui"),
			want:   want{conn: &connection{Version: v0124, Profile: minor012}, tracked: true},
		},
		"ClusterProviderConfig": {
			reason: "A managed resource can use a ClusterProviderConfig.",
			objs:   []kclient.Object{clusterProviderConfig(), secret("crossplane-system", "owui-key", map[string]string{tokenKey: apiKey})},
			mg:     managed(v1alpha1.ClusterProviderConfigKind, "default"),
			want:   want{conn: &connection{Version: v0124, Profile: minor012}, tracked: true},
		},
		"MissingProviderConfig": {
			reason: "A ProviderConfig of another namespace is not found.",
			objs:   []kclient.Object{providerConfig(func(pc *v1alpha1.ProviderConfig) { pc.Namespace = "other" })},
			mg:     managed(v1alpha1.ProviderConfigKind, "owui"),
			want:   want{tracked: true, msg: `cannot get ProviderConfig "owui"`},
		},
		"MissingClusterProviderConfig": {
			reason: "A missing ClusterProviderConfig is an error naming it.",
			mg:     managed(v1alpha1.ClusterProviderConfigKind, "default"),
			want:   want{tracked: true, msg: `cannot get ClusterProviderConfig "default"`},
		},
		"UnknownKind": {
			reason: "A reference to another kind is an error.",
			mg:     managed("Other", "owui"),
			want:   want{tracked: true, msg: `unsupported provider config kind "Other"`},
		},
		"TrackError": {
			reason: "A usage that cannot be tracked stops the connection.",
			track:  errBoom,
			objs:   []kclient.Object{providerConfig(), secret("team", "owui-key", map[string]string{tokenKey: apiKey})},
			mg:     managed(v1alpha1.ProviderConfigKind, "owui"),
			want:   want{tracked: true, err: errBoom, msg: "cannot track the provider config usage"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			owui := &fakeOpenWebUI{version: v0124}
			tracked := false
			track := resource.ModernTrackerFn(func(context.Context, resource.ModernManaged) error {
				tracked = true
				return tc.track
			})
			c := NewConnector(kube(t, tc.objs...), WithNewDoer(owui.newDoer), WithUsageTracker(track))
			got, err := c.Connect(context.Background(), tc.mg)
			checkError(t, tc.reason, "Connect(...)", err, tc.want.err, tc.want.msg)
			if diff := cmp.Diff(tc.want.conn, summarize(got)); diff != "" {
				t.Errorf("\n%s\nConnect(...): -want, +got:\n%s", tc.reason, diff)
			}
			if tracked != tc.want.tracked {
				t.Errorf("\n%s\nConnect(...): tracked usage %t, want %t", tc.reason, tracked, tc.want.tracked)
			}
		})
	}
}

func TestConnectConfigReusesTheDoer(t *testing.T) {
	cases := map[string]struct {
		reason string
		change func(kclient.Client, *v1alpha1.ProviderConfig)
		builds int
	}{
		"Unchanged": {
			reason: "Connecting twice with the same settings builds one Doer, so connections are reused and the insecure warning is logged once.",
			change: func(kclient.Client, *v1alpha1.ProviderConfig) {},
			builds: 1,
		},
		"ChangedSettings": {
			reason: "A changed setting builds a new Doer.",
			change: func(_ kclient.Client, pc *v1alpha1.ProviderConfig) { pc.Spec.AuthHeader = "x-api-key" },
			builds: 2,
		},
		"RotatedKey": {
			reason: "A rotated API key builds a new Doer.",
			change: func(kc kclient.Client, _ *v1alpha1.ProviderConfig) {
				if err := kc.Update(context.Background(), secret("team", "owui-key", map[string]string{tokenKey: apiKey + "2"})); err != nil {
					t.Fatal(err)
				}
			},
			builds: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			owui := &fakeOpenWebUI{version: v0124}
			builds := 0
			newDoer := func(cfg transport.Config) (client.Doer, error) {
				builds++
				return owui.newDoer(cfg)
			}
			kc := kube(t, secret("team", "owui-key", map[string]string{tokenKey: apiKey}))
			c := NewConnector(kc, WithNewDoer(newDoer))
			pc := providerConfig()
			if _, err := c.ConnectConfig(context.Background(), pc); err != nil {
				t.Fatal(err)
			}
			tc.change(kc, pc)
			if _, err := c.ConnectConfig(context.Background(), pc); err != nil {
				t.Fatal(err)
			}
			if builds != tc.builds {
				t.Errorf("\n%s\nConnectConfig(...) twice: built %d Doers, want %d", tc.reason, builds, tc.builds)
			}
		})
	}
}
