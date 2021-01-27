go run ./pub.go -s localhost:4223 -c \
"test-cluster" \
"com.hotellistat.live-scraping" \
'{
"specversion":"1.0",
"type":"com.hotellistat.live-scraping",
"id":"'"$(uuidgen)"'",
"source":"testing",
"data":{
  "job": "'"$(uuidgen)"'",
  "group": "'"$(uuidgen)"'",
  "id_ota": 1,
  "id_hotel": 2260,
  "hotel_ota_id": "de/grand-binz-ostseebad-binz.de.html",
  "crawl_date": "2021-04-15",
  "days_to_crawl": 1,
  "length_of_stay": 1,
  "max_persons": 2,
  "country_code": "de",
  "currency": "EUR",
  "closures": []
}}'