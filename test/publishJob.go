// package test

// import (
// 	"log"

// 	nats "github.com/nats-io/nats.go"
// 	"github.com/nats-io/stan.go"
// )

// func te() {
// 	nc, err := nats.Connect("localhost:4223")
// 	if err != nil {
// 		log.Fatal(err)
// 	}
// 	defer nc.Close()

// 	sc, err := stan.Connect("test-cluster", "testsetests123123", stan.NatsConn(nc))
// 	if err != nil {
// 		log.Fatalf("Can't connect stan")
// 	}
// 	defer sc.Close()
// }
