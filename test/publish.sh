go run ./pub.go -s localhost:4223 -c \
"test-cluster" \
"test" \
'{"specversion":"1.0","type":"com.hotellistat.crawling","id":"'"$(uuidgen)"'","source":"testing","data":{"type":"auto","identifier":"MlveoRrxcfKHsw9yL4tyHQHJ5aBxcMoj","ota_id":1,"hotel_id":1919,"hotel_ota_id":"de/remscheid.sk.html","offset":0,"crawl_date":"2021-01-18","days_to_crawl":10,"length_of_stay":1,"max_persons":2,"country_code":"de","currency":"EUR","date_current_day":"2021-02-17","date_next_day":"2021-02-18","closures":[]}}'