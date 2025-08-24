package main

import (
	"math"
	"os"
	"strconv"
)

var memLimit int = calcMemLimit()

func calcMemLimit() int {
	if e := os.Getenv("BEGB"); e != "" {
		f, err := strconv.ParseFloat(e, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
			panic("malformed BEGB environment variable, should be a number of gigabytes: " + e)
		}
		return int(f * 1024 * 1024 * 1024)
	}
	return 1024 * 1024 * 1024 // fall back on 1GiB
}
