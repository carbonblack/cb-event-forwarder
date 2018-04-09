Kafka-Util
An interim utliity for Cb Event Forwarder logs into Kafka, until reconsumable outputs are added to the architecture. 
This utility is intended to be used to move backup eventforarder output files into a .10+ kafka broker. 
Assuming you have been writing event forwarder log files to $SOMEDIR/cb/*.json: 
./kafka-util -BrokerList localhost:9092 -topicSuffix frombackup $SOMEDIR/cb/*.json
The created topics are based on the 'type' in the incoming JSON event. "ingress.event" is removed and the optional topic suffix will be appended if specified.  
The requestMaxSize paramter controls the sarama.MaxRequestSize parameter.

# Example run
```
$ ./kafka-util -BrokerList localhost:9092 -topicSuffix test ./jsonfiles/event_bridge_output.*  
INFO[0000] Kafka Utility:                               
INFO[0000] Files: [./jsonfiles/event_bridge_output.json.2 ./jsonfiles/event_bridge_output.json.2018-01-03T11:55:29.849.restart] 
INFO[0000] Brokers: [localhost:9092]                    
INFO[0000] Topic_Suffix: test                           
INFO[0000] Setup Ok                                     
INFO[0000] Opened file ./jsonfiles/event_bridge_output.json.2 
INFO[0000] Done scanning file %!(EXTRA string=./jsonfiles/event_bridge_output.json.2) 
INFO[0000] Opened file ./jsonfiles/event_bridge_output.json.2018-01-03T11:55:29.849.restart 
INFO[0000] Done scanning file %!(EXTRA string=./jsonfiles/event_bridge_output.json.2018-01-03T11:55:29.849.restart) 
```

#Broker topics after uploads are done:
```
$ /usr/local/Cellar/kafka/0.11.0.1/bin/kafka-topics --zookeeper localhost:2181 --list
alert.watchlist.hit.query.processtest
childproctest
feed.synchronizedtest
filemodtest
moduleloadtest
netconntest
procendtest
procstarttest
watchlist.hit.processtest
watchlist.storage.hit.processtest
```






