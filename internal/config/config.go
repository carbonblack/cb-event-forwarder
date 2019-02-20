package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "expvar"
	"fmt"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2/jwt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"strings"
)

func LoadFile(filename string) (map[string]interface{}, error) {
	var conf map[string]interface{} = make(map[string]interface{})
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return conf, err
	}
	err = yaml.Unmarshal(content, &conf)
	if err != nil {
		var i interface{}
		yaml.Unmarshal(content, &i)
		switch t := i.(type) {
		default:
			log.Debugf(fmt.Sprintf("Real type of yaml object is %T %s", t, i))
		}
		return conf, err
	}
	return conf, err
}

func ParseConfig(fn string) (map[string]interface{}, error) {
	//errs := ConfigurationError{Empty: true}
	config, err := LoadFile(fn)
	log.Debugf("configs from load file = %s \n err = %s \n ", config, err)
	return config, err
}

// tls configs are specified in a dictionary like tls : { tls_client_cert : mycert.cert , tls_clinet_key : mykey.key , tls_verify : true }
func GetTLSConfigFromCfg(cfg map[interface{}]interface{}) (*tls.Config, error) {

	tlsConfig := &tls.Config{}

	if tlsverifyT, ok := cfg["verify"]; ok {
		tlsConfig.InsecureSkipVerify = !tlsverifyT.(bool)
	} else {
		log.Warn("!!!Disabling TLS verification!!!")
		tlsConfig.InsecureSkipVerify = true
	}

	tlsClientCert := ""
	if tlsClientCertTemp, ok := cfg["client_cert"]; ok {
		tlsClientCert = tlsClientCertTemp.(string)
	}
	tlsClientKey := ""
	if tlsClientKeyTemp, ok := cfg["client_key"]; ok {
		tlsClientKey = tlsClientKeyTemp.(string)
	}
	tlsCACert := ""
	if tlsCACertTemp, ok := cfg["ca_cert"]; ok {
		tlsCACert = tlsCACertTemp.(string)
	}
	tlsCName := ""
	if tlsCNameTemp, ok := cfg["cname"]; ok {
		tlsCName = tlsCNameTemp.(string)
	}
	tls12Only := true
	if tls12OnlyTemp, ok := cfg["tls_12_only"]; ok {
		tls12Only = tls12OnlyTemp.(bool)
	}

	if tlsClientCert != "" && tlsClientKey != "" {
		log.Infof("Loading client cert/key from %s & %s", tlsClientCert, tlsClientKey)
		cert, err := tls.LoadX509KeyPair(tlsClientCert, tlsClientKey)
		if err != nil {
			log.Errorf("error loading client cert/key from %s & %s : %v", tlsClientCert, tlsClientKey, err)
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if tlsCACert != "" && len(tlsCACert) > 0 {
		// Load CA cert
		log.Infof("Loading valid CAs from file %s", tlsCACert)
		caCert, err := ioutil.ReadFile(tlsCACert)
		if err != nil {
			log.Errorf("error Loading valid CAs from file %s : %v", tlsCACert)
			return nil, err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	if tlsCName != "" && len(tlsCName) > 0 {
		log.Infof("Forcing TLS Common Name check to use '%s' as the hostname", tlsCName)
		tlsConfig.ServerName = tlsCName
	}

	if tls12Only == true {
		log.Info("Enforcing minimum TLS version 1.2")
		tlsConfig.MinVersion = tls.VersionTLS12
	} else {
		log.Warn("Relaxing minimum TLS version to 1.0")
		tlsConfig.MinVersion = tls.VersionTLS10
	}

	return tlsConfig, nil
}

// jwt cfg getter
// jwt:
//	email:
//	privatekey:
//	privatekeyid:
//	scopes:
//	tokenurl:
/*
OAuthJwtClientEmail    string
	OAuthJwtPrivateKey     []byte
	OAuthJwtPrivateKeyId   string
	OAuthJwtScopes         []string
	OAuthJwtTokenUrl       string
*/
// parseOAuthConfiguration parses OAuth related configuration from input and populates config with
// relevant fields.
/*
jwt:
    client_email: bafdafasf
    private_key: afdasfdsa
    private_key_id: afdasf
    scopes: afdas,afdsafdas,fdas
    token_url: afdasfdsa.cafdsa

*/
func GetJWTConfigFromCfg(cfg map[interface{}]interface{}) (*jwt.Config, error) {

	// oAuthFieldsConfigured is used to track OAuth fields that have been configured.
	oAuthFieldsConfigured := make(map[string]bool)
	var err error = nil
	var oAuthJwtScopes []string = make([]string, 0)
	var oAuthJwtClientEmail string = ""
	var oAuthJwtPrivateKey string = ""
	var oAuthJwtPrivateKeyId string = ""
	var oAuthJwtTokenUrl string = ""

	if oAuthJwtClientEmail, ok := cfg["client_email"].(string); ok {
		if len(oAuthJwtClientEmail) > 0 {
			oAuthFieldsConfigured["oauth_jwt_client_email"] = true
		} else {
			err = errors.New("Empty value is specified for oauth_jwt_client_email")
		}
	}

	if oAuthJwtPrivateKey, ok := cfg["private_key"].(string); ok {
		if len(oAuthJwtPrivateKey) > 0 {
			oAuthFieldsConfigured["oauth_jwt_private_key"] = true
			// Replace the escaped version of a line break character with the non-escaped version.
			// The OAuth library which reads private key expects non-escaped version of line break
			// character.
			oAuthJwtPrivateKey = strings.Replace(oAuthJwtPrivateKey, "\\n", "\n", -1)
			//config.OAuthJwtPrivateKey = []byte(oAuthJwtPrivateKey)
		} else {
			err = errors.New("Empty value is specified for oauth_jwt_private_key")
		}
	}

	if oAuthJwtPrivateKeyId, ok := cfg["private_key_id"].(string); ok {
		if len(oAuthJwtPrivateKeyId) > 0 {
			oAuthFieldsConfigured["oauth_jwt_private_key_id"] = true
		} else {
			err = errors.New("Empty value is specified for oauth_jwt_private_key_id")
		}
	}

	if scopesStr, ok := cfg["scopes"].(string); ok {
		if len(scopesStr) > 0 {
			oAuthFieldsConfigured["oauth_jwt_scopes"] = true

			for _, scope := range strings.Split(scopesStr, ",") {
				scope = strings.TrimSpace(scope)
				if len(scope) > 0 {
					oAuthJwtScopes = append(oAuthJwtScopes, scope)
				} else {
					err = errors.New("Empty scope found")
				}
			}
		} else {
			err = errors.New("Empty value is specified for oauth_jwt_scopes")
		}

	}

	if oAuthJwtTokenUrl, ok := cfg["token_url"].(string); ok {
		if len(oAuthJwtTokenUrl) > 0 {
			oAuthFieldsConfigured["oauth_jwt_token_url"] = true
		} else {
			err = errors.New("Empty value is specified for oauth_jwt_token_url")
		}
	}

	// requiredOAuthFields contains the fields that must be present if OAuth is configured.
	requiredOAuthFields := []string{
		"oauth_jwt_client_email",
		"oauth_jwt_private_key",
		"oauth_jwt_token_url",
	}

	// Check that all required fields present if OAuth is configured.
	if len(oAuthFieldsConfigured) > 0 {
		for _, requiredField := range requiredOAuthFields {
			errstr := ""
			if !oAuthFieldsConfigured[requiredField] {
				errstr += fmt.Sprintf("\nRequired OAuth field %s is not configured", requiredField)
				err = errors.New(errstr)
			}

		}
	}

	jwtConfig := &jwt.Config{
		Email:        oAuthJwtClientEmail,
		PrivateKey:   []byte(oAuthJwtPrivateKey),
		PrivateKeyID: oAuthJwtPrivateKeyId,
		Scopes:       oAuthJwtScopes,
		TokenURL:     oAuthJwtTokenUrl,
	}

	return jwtConfig, err
}
