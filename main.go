package main

import (
	"cfspeedtest/speedtest"
)

func main() {
	upload_tests := []speedtest.Test{
		{NumBytes: 101000, Iterations: 8, Name: "100kB"},
		{NumBytes: 1001000, Iterations: 6, Name: "1MB"},
		{NumBytes: 10001000, Iterations: 4, Name: "10MB"},
	}

	download_tests := []speedtest.Test{
		{NumBytes: 101000, Iterations: 10, Name: "100kB"},
		{NumBytes: 1001000, Iterations: 8, Name: "1MB"},
		{NumBytes: 10001000, Iterations: 6, Name: "10MB"},
	}

	test := speedtest.NewSpeedtest(upload_tests, download_tests)
	test.RunAllTests()
}
