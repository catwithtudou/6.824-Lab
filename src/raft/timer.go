package main

import (
	"math/rand"
	"time"
)

const (
	HeartbeatInterval    = time.Duration(150) * time.Millisecond
	ElectionTimeoutLower = time.Duration(300) * time.Millisecond
	ElectionTimeoutUpper = time.Duration(400) * time.Millisecond
)

func ElectionTimeDuration() time.Duration {
	return time.Duration(rand.Int63n(ElectionTimeoutUpper.Nanoseconds()-ElectionTimeoutLower.Nanoseconds())+ElectionTimeoutLower.Nanoseconds()) * time.Nanosecond
}

func ResetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}
