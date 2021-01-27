package app

// import (
// 	"log"

// 	"github.com/nats-io/nats-streaming-server/server"
// 	"github.com/nats-io/stan.go"
// )

// func RunServer(ID string) *server.StanServer {
// 	s, err := server.RunServer(ID)
// 	if err != nil {
// 		panic(err)
// 	}
// 	return s
// }

// const (
// 	clusterName = "test_cluster"
// 	clientName  = "test_client"
// )

// type tLogger interface {
// 	Fatalf(format string, args ...interface{})
// 	Errorf(format string, args ...interface{})
// }

// func NewDefaultConnection(t tLogger) stan.Conn {
// 	sc, err := stan.Connect(clusterName, clientName)
// 	if err != nil {
// 		log.Fatalln("Could not connect to nats server")
// 	}
// 	return sc
// }
