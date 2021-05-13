import pika
import uuid
import json
import time


connection = pika.BlockingConnection(
    pika.ConnectionParameters(
        'localhost',
        5672,
        credentials=pika.PlainCredentials("main", "OGQXDO2I39")
        ))
channel = connection.channel()


for i in range(1):
    message = {
        "specversion": "1.0",
        "type": "com.hotellistat.revenue-module",
        "id": str(uuid.uuid4()),
        "source": "testing",
        "nopublish": True,
        "data": {
           "hotel": 1914
        }
    }
    channel.basic_publish(
        exchange='',
        routing_key='com.hotellistat.revenue-module',
        body=json.dumps(message)
    )


connection.close()
