package config

import (
	"crypto/tls"
	"crypto/x509"
	_ "expvar"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io/ioutil"
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
