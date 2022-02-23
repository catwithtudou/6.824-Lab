package main

import "log"

// Debugging
const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

// min

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
