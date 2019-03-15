module github.com/carbonblack/cb-event-forwarder

replace gopkg.in/h2non/filetype.v1 v1.0.8 => github.com/h2non/filetype v1.0.8

go 1.12

require (
	github.com/RackSec/srslog v0.0.0-20180709174129-a4725f04ec91
	github.com/aws/aws-sdk-go v1.18.2
	github.com/colinmarc/hdfs v1.1.3
	github.com/confluentinc/confluent-kafka-go v0.11.6
	github.com/fsnotify/fsnotify v1.4.7 // indirect
	github.com/gogo/protobuf v1.2.1
	github.com/golang/protobuf v1.3.0
	github.com/gregjones/httpcache v0.0.0-20190212212710-3befbb6ad0cc
	github.com/h2non/filetype v1.0.8
	github.com/hpcloud/tail v1.0.0
	github.com/kisielk/errcheck v1.2.0 // indirect
	github.com/sirupsen/logrus v1.4.0
	github.com/streadway/amqp v0.0.0-20190312223743-14f78b41ce6d
	github.com/stretchr/testify v1.3.0
	github.com/vaughan0/go-ini v0.0.0-20130923145212-a98ad7ee00ec
	golang.org/x/crypto v0.0.0-20190313024323-a1f597ede03a // indirect
	golang.org/x/net v0.0.0-20190313220215-9f648a60d977 // indirect
	golang.org/x/oauth2 v0.0.0-20190226205417-e64efc72b421
	golang.org/x/sys v0.0.0-20190312061237-fead79001313 // indirect
	golang.org/x/tools v0.0.0-20190314010720-1286b2016bb1 // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.2.2
	zvelo.io/ttlru v1.0.6
)
