import pika
import uuid
import json
import time


connection = pika.BlockingConnection(
    pika.ConnectionParameters(
        'localhost',
        5672,
    ))
channel = connection.channel()


for i in range(1):
    message = {
        "specversion": "1.0",
        "type": "com.yourorg.some-queue",
        "id": str(uuid.uuid4()),
        "source": "batchable-development",
        "data": {
            "key": "value"
        }

    }
    channel.basic_publish(
        exchange='',
        properties=pika.BasicProperties(
            delivery_mode=2,
        ),
        routing_key='com.yourorg.some-queue',
        body=json.dumps(message)
    )


connection.close()
