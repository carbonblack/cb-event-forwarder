#pip install confluent-kafka
import sys
import glob
from confluent_kafka import Producer

def main(args):
    brokers = args[0].split(",")
    path = args[1]
    files = glob.glob(path)
    topic_suffix = "" if len(args) < 3 else args[2]
    p = Producer({'bootstrap.servers': brokers)})
    for f in files: 
        for data in open(f,'r').readlines():
                data_dict = json.loads(data)
                p.produce(topic_suffix+data_dict['type'].replace('ingress.event',''), data.encode('utf-8'))
                p.flush()
        
if __name__ == "__main__":
        #./kafka_util.py brokers-list path-to-files optional-topic-suffix
        #ex) ./kafka_util.py localhost:2181,localhost:31337 /path/to/output*.json
        main(sys.argv)
