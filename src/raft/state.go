package main


type StatePeer uint

const (
	Follower  = StatePeer(1)
	Candidate = StatePeer(2)
	Leader    = StatePeer(3)
)
