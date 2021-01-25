go run ./pub.go -s localhost:4223 -c \
"test-cluster" \
"test" \
'{"specversion":"1.0","type":"com.hotellistat.parsing","id":"'"$(uuidgen)"'","source":"testing","data":{"type":"auto","identifier":"aaaaaaaaaa","ota_id":1,"hotel_id":1919,"crawl_date":"2021-01-18","length_of_stay":1,"max_persons":2}}'