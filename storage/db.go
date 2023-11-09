package storage

type State struct {
	NetworkName        string
	LowestBlockNumber  uint64
	HighestBlockNumber uint64
}

type StateProvider interface {
	State() State
	SetState(func(*State) State) error
}
