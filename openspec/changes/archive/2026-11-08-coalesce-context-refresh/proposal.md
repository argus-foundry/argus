# Independent context refresh

Context events currently clear valid categories and wait for unrelated account reads. Give the context measurement and auxiliary usage reads separate cancellation and publication ownership. Debounce same-session event bursts, share overlapping reads, and retain valid displayed values until a new result settles. Task and session replacement still clear immediately.

Task context measurements own category data. Auxiliary usage owns bridge quota, account and backend data on its existing cadence. Legacy metadata is read only after an unsupported context route. This scope was directed by em in Hera message 1319. No daemon or other frontend changes.
