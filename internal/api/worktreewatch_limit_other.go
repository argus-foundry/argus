//go:build !darwin

package api

func wtProcessWatchLimit() int { return wtMaxWatches * 2 }
