# pprof Modes Reference

Quick reference for interpreting `profile analyze` output.

## Top Mode (`--mode=top`)

Default mode. Shows a ranked list of functions by resource consumption.

```
      flat  flat%   sum%        cum   cum%
    1.20s 30.00% 30.00%      1.50s 37.50%  github.com/gadget-inc/skipper/internal/hashring.(*HashRing).Get
```

| Column    | Meaning                                                  |
| --------- | -------------------------------------------------------- |
| **flat**  | Time spent directly in this function (excluding callees) |
| **flat%** | flat as percentage of total profile duration             |
| **sum%**  | Running sum of flat% (cumulative down the list)          |
| **cum**   | Time spent in this function including all callees        |
| **cum%**  | cum as percentage of total profile duration              |

**Reading tip**: High flat% = the function itself is expensive. High cum% with low flat% = the function calls expensive things.

### Sorting

- Default (`--mode=top`): sorted by flat time — "where does the CPU actually execute?"
- With `--cum`: sorted by cumulative time — "which call trees are most expensive?"

## Peek Mode (`--mode=peek -f <regex>`)

Shows the callers and callees of matched functions.

```
Showing nodes accounting for 4s, 100% of 4s total
----------------------------------------------------------+-------
                                             1.20s   100% |   github.com/gadget-inc/skipper/internal/router.(*Router).handleRequest
    1.20s 30.00%                                          | github.com/gadget-inc/skipper/internal/hashring.(*HashRing).Get
                                             0.30s 25.00% |   hash/crc32.ChecksumIEEE
                                             0.20s 16.67% |   slices.BinarySearch
----------------------------------------------------------+-------
```

**Reading tip**: Lines above the function show callers (with how much time they contribute). Lines below show callees (with how much time they consume).

## Source Mode (`--mode=source -f <regex>`)

Shows source code annotated with time attribution per line.

```
ROUTINE ======================== github.com/gadget-inc/skipper/internal/hashring.(*HashRing).Get
    1.20s      1.50s (flat, cum) 37.50% of Total
         .          .     137:func (h *HashRing) Get(value RingKey) string {
         .          .     138:	rt := h.mu.RLock()
      20ms       20ms    161:	hash := crc32.ChecksumIEEE([]byte(value.RingKey()))
     800ms      800ms    164:	index, found := slices.BinarySearch(h.hashes, hash)
```

**Reading tip**: First column is flat time for that line, second is cumulative. Lines with `.` consumed negligible time.

## Diff Mode (`--mode=diff --diff-base=<file>`)

Compares two profiles. Positive values mean the new profile is slower; negative values mean it improved.

```
      flat  flat%   sum%        cum   cum%
    -0.50s -12.50% -12.50%     -0.50s -12.50%  github.com/gadget-inc/skipper/internal/hashring.(*HashRing).Get
     0.30s   7.50%  -5.00%      0.30s   7.50%  runtime.mallocgc
```

**Reading tip**: Focus on large negative values (improvements) and large positive values (regressions). Small changes may be noise.
