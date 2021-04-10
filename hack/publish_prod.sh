go run ./pub.go -s nats.messaging:4222 -c \
"nats" \
"com.hotellistat.scraping" \
'{"specversion":"1.0",
"type":"com.hotellistat.scraping",
"id":"'"$(uuidgen)"'",
"source":"testing",
"data":{
  "type":"auto",
  "identifier":"'"$(uuidgen)"'",
  "ota_id":5,
  "hotel_id":17,
  "hotel_ota_id":"391295",
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