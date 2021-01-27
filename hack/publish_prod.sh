go run ./pub.go -s nats.nats:4222 -c \
"nats" \
"com.hotellistat.scraping" \
'{"specversion":"1.0",
"type":"com.hotellistat.scraping",
"id":"'"$(uuidgen)"'",
"source":"testing",
"data":{
  "type":"auto",
  "identifier":"'"$(uuidgen)"'",
  "ota_id":1,
  "hotel_id":1919,
  "hotel_ota_id":"de/remscheid.sk.html",
  "offset":0,
  "crawl_date":"2021-01-26",
  "days_to_crawl":100,
  "length_of_stay":1,
  "max_persons":2,
  "country_code":"de",
  "currency":"EUR",
  "closures":[]
  }
}'