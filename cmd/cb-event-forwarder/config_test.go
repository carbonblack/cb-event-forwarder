package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	ini "github.com/vaughan0/go-ini"
)

func TestParseOAuthConfiguration(t *testing.T) {
	for _, test := range []struct {
		desc           string
		input          *ini.File
		config         *Configuration
		errs           *ConfigurationError
		expectedConfig *Configuration
		expectedErrs   *ConfigurationError
	}{
		{
			desc: "All OAuth fields configured",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKey:   []byte("private_key"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1", "scope2"},
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{Empty: true},
		},
		{
			desc: "Only required OAuth fields configured",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email": "example@serviceaccount.com",
					"oauth_jwt_private_key":  "private_key",
					"oauth_jwt_token_url":    "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail: "example@serviceaccount.com",
				OAuthJwtPrivateKey:  []byte("private_key"),
				OAuthJwtTokenUrl:    "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{Empty: true},
		},
		{
			desc: "Replace the escaped version of line break character to the non-escaped version",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "-----BEGIN PRIVATE KEY-----\\nVcgdkPBHC\\n-----END PRIVATE KEY-----\\n",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKey:   []byte("-----BEGIN PRIVATE KEY-----\nVcgdkPBHC\n-----END PRIVATE KEY-----\n"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1", "scope2"},
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{Empty: true},
		},
	} {
		test := test // capture range variable.
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			errs := test.errs
			config := test.config
			parseOAuthConfiguration(test.input, config, errs)

			if diff := cmp.Diff(config, test.expectedConfig); diff != "" {
				t.Errorf("config different from expected, diff: %s", diff)
			}

			if diff := cmp.Diff(errs, test.expectedErrs); diff != "" {
				t.Errorf("errors different from expected, diff: %s", diff)
			}
		})
	}
}

func TestParseOAuthConfigurationErrors(t *testing.T) {
	for _, test := range []struct {
		desc           string
		input          *ini.File
		config         *Configuration
		errs           *ConfigurationError
		expectedConfig *Configuration
		expectedErrs   *ConfigurationError
	}{
		{
			desc: "Empty value for oauth_jwt_client_email",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtPrivateKey:   []byte("private_key"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1", "scope2"},
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty value is specified for oauth_jwt_client_email",
					"Required OAuth field oauth_jwt_client_email is not configured",
				},
			},
		},
		{
			desc: "Empty value for oauth_jwt_private_key",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1", "scope2"},
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty value is specified for oauth_jwt_private_key",
					"Required OAuth field oauth_jwt_private_key is not configured",
				},
			},
		},
		{
			desc: "Empty value for oauth_jwt_private_key_id",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail: "example@serviceaccount.com",
				OAuthJwtPrivateKey:  []byte("private_key"),
				OAuthJwtScopes:      []string{"scope_1", "scope2"},
				OAuthJwtTokenUrl:    "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty value is specified for oauth_jwt_private_key_id",
				},
			},
		},
		{
			desc: "Empty value for oauth_jwt_scopes",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKey:   []byte("private_key"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty value is specified for oauth_jwt_scopes",
				},
			},
		},
		{
			desc: "Empty value for oauth_jwt_token_url",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1,scope2",
					"oauth_jwt_token_url":      "",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKey:   []byte("private_key"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1", "scope2"},
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty value is specified for oauth_jwt_token_url",
					"Required OAuth field oauth_jwt_token_url is not configured",
				},
			},
		},
		{
			desc: "Missing required field: oauth_jwt_client_email",
			input: &ini.File{
				"http": {
					"oauth_jwt_private_key": "private_key",
					"oauth_jwt_token_url":   "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtPrivateKey: []byte("private_key"),
				OAuthJwtTokenUrl:   "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Required OAuth field oauth_jwt_client_email is not configured",
				},
			},
		},
		{
			desc: "Missing required field: oauth_jwt_private_key",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email": "example@serviceaccount.com",
					"oauth_jwt_token_url":    "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail: "example@serviceaccount.com",
				OAuthJwtTokenUrl:    "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Required OAuth field oauth_jwt_private_key is not configured",
				},
			},
		},
		{
			desc: "Missing required field: oauth_jwt_token_url",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email": "example@serviceaccount.com",
					"oauth_jwt_private_key":  "private_key",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail: "example@serviceaccount.com",
				OAuthJwtPrivateKey:  []byte("private_key"),
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Required OAuth field oauth_jwt_token_url is not configured",
				},
			},
		},
		{
			desc: "oauth_jwt_scopes contains empty scope",
			input: &ini.File{
				"http": {
					"oauth_jwt_client_email":   "example@serviceaccount.com",
					"oauth_jwt_private_key":    "private_key",
					"oauth_jwt_private_key_id": "private_key_id",
					"oauth_jwt_scopes":         "scope_1, ",
					"oauth_jwt_token_url":      "https://example.com/oauth2/token",
				},
			},
			config: &Configuration{},
			errs:   &ConfigurationError{Empty: true},
			expectedConfig: &Configuration{
				OAuthJwtClientEmail:  "example@serviceaccount.com",
				OAuthJwtPrivateKey:   []byte("private_key"),
				OAuthJwtPrivateKeyId: "private_key_id",
				OAuthJwtScopes:       []string{"scope_1"},
				OAuthJwtTokenUrl:     "https://example.com/oauth2/token",
			},
			expectedErrs: &ConfigurationError{
				Errors: []string{
					"Empty scope found",
				},
			},
		},
	} {
		test := test // capture range variable.
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			errs := test.errs
			config := test.config
			parseOAuthConfiguration(test.input, config, errs)

			if diff := cmp.Diff(config, test.expectedConfig); diff != "" {
				t.Errorf("config different from expected, diff: %s", diff)
			}

			if diff := cmp.Diff(errs, test.expectedErrs); diff != "" {
				t.Errorf("errors different from expected, diff: %s", diff)
			}
		})
	}
}
