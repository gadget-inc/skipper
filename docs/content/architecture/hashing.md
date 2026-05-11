---
title: Hashing
description: How Skipper identifies assignments and distributes responsibility across controllers using consistent hashing.
---

Skipper uses hashing at two levels: to uniquely identify each assignment, and to distribute assignment ownership across controller replicas via a consistent hash ring.

## Assignment hashing

An assignment is uniquely identified by four fields: namespace, deployment, tenant, and oneshot flag. These are hashed together using xxhash to produce an `AssignmentHash` (uint64) that serves as the assignment's identity throughout the system.

```
hash = xxhash(namespace + NUL + deployment + NUL + tenant + NUL + oneshot)
```

Fields are separated by null bytes (`NUL`, 0x00) to prevent collisions (e.g., namespace `ab` + deployment `cd` vs namespace `abc` + deployment `d`). The oneshot flag is encoded as a single byte (`0x01` or `0x00`).

The `Assignment` type is defined as protobuf in `internal/skipper/types.proto`. Policy fields (scale, retry, transport, etc.) are deliberately excluded from the hash — changing an assignment's scaling targets does not create a new identity.

## Hash ring

Skipper uses consistent hashing to distribute responsibility for assignments across controller replicas. Each controller IP gets 1024 virtual nodes on the ring, ensuring even distribution even with a small number of controllers.

### Hash computation

Each virtual node's position is computed with FNV-1a over `ip + ":" + index` (where index is a uint16), followed by a murmur3 fmix32 avalanche finalizer. The finalizer ensures even distribution across the 32-bit ring space.

### Lookup

To find the responsible controller for an assignment:

1. XOR-fold the 64-bit assignment hash down to 32 bits
2. Binary search the sorted ring for the next position (wraps around at the end)
3. Return the controller IP that owns that position

### Concurrency

The ring is protected by `xsync.RBMutex`, a read-biased mutex optimized for high-read workloads. The sorted IP list is cached and only invalidated on add/remove, with double-checked locking after acquiring the write lock.

### Startup behavior

If the ring is empty when `Get()` is called, the caller blocks for up to `--hash-ring-wait-time` (default: 10s) with 4 retry attempts. If the ring is still empty after all attempts, `Get()` panics -- an empty ring at that point indicates a fundamental discovery failure.
