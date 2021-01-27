go run ./pub.go -s nats.nats:4222 -c \
"nats" \
"com.hotellistat.parsing" \
'{"specversion":"1.0",
"type":"com.hotellistat.parsing",
"id":"'"$(uuidgen)"'",
"source":"testing",
"data":{
  "type":"auto",
  "identifier":"c889b139-cf35-4453-8d15-9f5480262fdf",
  "ota_id":1,
  "hotel_id":1919,
  "crawl_date":"2021-01-18",
  "length_of_stay":1,
  "max_persons":2
  }
}'