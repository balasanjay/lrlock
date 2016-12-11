A scalable RWMutex with minimal contention and wait-freedom for readers.

The downside is that it requires clients to store two copies of their
data-structures, and has a fixed base-cost of O(P) (where P is effectively
GOMAXPROCS), not O(1).
