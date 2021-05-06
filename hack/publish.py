import pika
import uuid
import json
import time


connection = pika.BlockingConnection(
    pika.ConnectionParameters('localhost', 5672))
channel = connection.channel()


for i in range(40):
    message = {
        "specversion": "1.0",
        "type": "com.hotellistat.scraping",
        "id": str(uuid.uuid4()),
        "source": "testing",
        "data": {
            "type": "auto",
            "identifier": str(uuid.uuid4()),
            "ota_id": 1,
            "hotel_id": 17,
            "hotel_ota_id": "de/rocco-forte-the-charles.de.html",
            "offset": 0,
            "crawl_date": "2021-05-01",
            "days_to_crawl": 1,
            "length_of_stay": 1,
            "max_persons": 2,
            "country_code": "de",
            "currency": "EUR",
            "closures": []
        }
    }
    channel.basic_publish(exchange='',
                      routing_key='com.hotellistat.scraping',
                      body=json.dumps(message))


connection.close()
