package api

import "golang.org/x/sys/unix"

func wtProcessWatchLimit() int {
	limit := wtMaxWatches * 2
	var r unix.Rlimit
	if unix.Getrlimit(unix.RLIMIT_NOFILE, &r) == nil {
		available := r.Cur / 4
		if available < wtMaxWatches*2 {
			limit = int(available)
		}
	}
	return limit
}
