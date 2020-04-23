import pika
import os
import time

def main():
    #cb = CbResponseAPI()

    #creds = cb.credentials
    url = os.environ['AMQPURL']

    print("Trying to connect to rabbit with URL " + url)

    #url = "amqp://{0}:{1}@{2}:{3}".format(creds.rabbitmq_user,
                                          #creds.rabbitmq_pass,
                                          #creds.rabbitmq_host,
                                          #creds.rabbitmq_port)

    parameters = pika.URLParameters(url)

    connection = pika.BlockingConnection(parameters)

    channel = connection.channel()

    with open('zipbundles/bundleone', 'rb') as file_handle:
        buffer = file_handle.read()

    if not buffer:
        print("Failed to read zipbundle")
        return

    while True:
        channel.basic_publish(exchange='api.rawsensordata',
                              routing_key='',
                              body=buffer,
                              properties=pika.BasicProperties(
                                  content_type='application/protobuf',
                                  delivery_mode=1))

        time.sleep(0.001)

    connection.close()


if __name__ == '__main__':
    main()
