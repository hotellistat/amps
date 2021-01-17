package shim

type BrokerShim interface {
	Initialize()
	Teardown()
	Start()
	Stop()
}
