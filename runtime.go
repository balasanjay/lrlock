package lrlock

func gomaxprocs() int {
	// TODO: implement me. Caveat, it should be fast, shouldn't cause cross-core
	// communication, and can be eventually consistent.
	return 4
}

func getp() int {
	// TODO: implement me. Caveat, it should be fast, and shouldn't cause cross-core
	// communication.
	return 0
}

func slotsPerCacheLineBits() uint {
	// TODO: implement me. Caveat, it should be fast, shouldn't cause cross-core
	// communication, and shouldln't change over the life time of the program.
	return 4 // Common cache line is 6 bits long, minus 2 bits for the slot size.
}
