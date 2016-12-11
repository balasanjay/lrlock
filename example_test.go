package lrlock_test

import (
	"fmt"

	"github.com/balasanjay/lrlock"
)

func ExampleLRMutex() {
	// This examples is intended to demonstrate using an LRMutex to protect a
	// global string variable (maybe the status string to display for a /healthz
	// handler). The getter and setter for this variable would ordinarily be
	// global functions, but are locally declared functions for the sake of the
	// example.
	var (
		globalMu lrlock.LRMutex
		global   [2]string
	)

	getGlobal := func() string {
		l := globalMu.RLock()
		g := global[l.Idx()]
		l.RUnlock()
		return g
	}

	setGlobal := func(s string) {
		for w := globalMu.Lock(); w.Next(); {
			global[w.Idx()] = s
		}
	}

	setGlobal("foobar")
	fmt.Println(getGlobal())
	// Output: foobar
}
