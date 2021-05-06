go run ./pub.go -s localhost:4222 -c \
"nats" \
"com.hotellistat.scraping.booking" \
'{"specversion":"1.0",
"type":"com.hotellistat.scraping.booking",
"id":"'"$(uuidgen)"'",
"source":"testing",
"data":{
  "type":"auto",
  "identifier":"'"$(uuidgen)"'",
  "ota_id":1,
  "hotel_id":17,
  "hotel_ota_id":"de/rocco-forte-the-charles.de.html",
  "offset":0,
  "crawl_date":"2021-05-01",
  "days_to_crawl":2,
  "length_of_stay":1,
  "max_persons":2,
  "country_code":"de",
  "currency":"EUR",
  "closures":[]
  }
}'