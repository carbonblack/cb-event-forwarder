package tests

import (
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	"github.com/carbonblack/cb-event-forwarder/pkg/outputs"
	"golang.org/x/oauth2"
	"net/http"
	"reflect"
	"testing"
)

func TestCreateTransport(t *testing.T) {
	for _, test := range []struct {
		desc         string
		config       Configuration
		expectedType reflect.Type
	}{
		{
			desc: "oauth2.Transport",
			config: Configuration{
				OAuthJwtClientEmail: "example@serviceaccount.com",
				OAuthJwtPrivateKey:  []byte("private_key"),
				OAuthJwtTokenUrl:    "https://example.com/oauth2/token",
			},
			expectedType: reflect.TypeOf(&oauth2.Transport{}),
		},
		{
			desc:         "http.Transport",
			config:       Configuration{},
			expectedType: reflect.TypeOf(&http.Transport{}),
		},
	} {
		test := test // capture range variable.
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			httpBehavior := outputs.HTTPBehavior{Config: &test.config}
			transport := httpBehavior.CreateTransport()
			if reflect.TypeOf(transport) != test.expectedType {
				t.Errorf("type of transport: %s, want: %s", reflect.TypeOf(transport), test.expectedType)
			}
		})
	}
}
