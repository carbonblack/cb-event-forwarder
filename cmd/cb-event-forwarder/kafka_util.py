#pip install kafka-python
import sys
import glob
import json
from kafka import KafkaProducer

def main(args):
    arg_len = len(args)
    if arg_len < 2:
        print ("usage: kafka_util.py broker1:port1,broker2,port2 /path/to/json/files/like_*.json optional_topic_suffix")
        exit()
    brokers = args[0]
    path = args[1]
    files = glob.glob(path)
    topic_suffix = "" if len(args) < 3 else args[2]
    p = KafkaProducer(bootstrap_servers=brokers,value_serializer=lambda s: s.encode('utf-8'))
    for f in files: 
        for data in open(f,'r').readlines():
                data_dict = json.loads(data)
                p.send(data_dict['type'].replace('ingress.event','')+topic_suffix, data)
                p.flush()
        
if __name__ == "__main__":
        #./kafka_util.py brokers-list path-to-files optional-topic-suffix
        #ex) ./kafka_util.py localhost:2181,localhost:31337 /path/to/output*.json
        main(sys.argv)
