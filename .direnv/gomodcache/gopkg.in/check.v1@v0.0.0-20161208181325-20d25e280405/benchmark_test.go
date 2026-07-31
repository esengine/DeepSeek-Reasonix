// These tests verify the test running logic.

package check_test

import (
	"time"
	. "gopkg.in/check.v1"
)

var benchmarkS = Suite(&BenchmarkS{})

type BenchmarkS struct{}

func (s *BenchmarkS) TestCountSuite(c *C) {
	suitesRun += 1
}

func (s *BenchmarkS) TestBasicTestTiming(c *C) {
	helper := FixtureHelper{sleepOn: "Test1", sleep: 1000000 * time.Nanosecond}
	output := String{}
	runConf := RunConf{Output: &output, Verbose: true}
	Run(&helper, &runConf)

	expected := "PASS: check_test\\.go:[0-9]+: FixtureHelper\\.Test1\t0\\.0[0-9]+s\n" +
		"PASS: check_test\\.go:[0-9]+: FixtureHelper\\.Test2\t0\\.0[0-9]+s\n"
	c.Assert(output.value, Matches, expected)
}

func (s *BenchmarkS) TestStreamTestTiming(c *C) {
	helper := FixtureHelper{sleepOn: "SetUpSuite", sleep: 1000000 * time.Nanosecond}
	output := String{}
	runConf := RunConf{Output: &output, Stream: true}
	Run(&helper, &runConf)

	expected := "(?s).*\nPASS: check_test\\.go:[0-9]+: FixtureHelper\\.SetUpSuite\t[0-9]+\\.[0-9]+s\n.*"
	c.Assert(output.value, Matches, expected)
}

func (s *BenchmarkS) TestBenchmark(c *C) {
	helper := FixtureHelper{sleep: 100000}
	output := String{}
	runConf := RunConf{
		Output:        &output,
		Benchmark:     true,
		BenchmarkTime: 10000000,
		Filter:        "Benchmark1",
	}
	Run(&helper, &runConf)
	c.Check(helper.calls[0], Equals, "SetUpSuite")
	c.Check(helper.calls[1], Equals, "SetUpTest")
	c.Check(helper.calls[2], Equals, "Benchmark1")
	c.Check(helper.calls[3], Equals, "TearDownTest")
	c.Check(helper.calls[4], Equals, "SetUpTest")
	c.Check(helper.calls[5], Equals, "Benchmark1")
	c.Check(helper.calls[6], Equals, "TearDownTest")
	// ... and more.

	expected := "PASS: check_test\\.go:[0-9]+: FixtureHelper\\.Benchmark1\t\\s+[0-9]+\t\\s+[0-9]+ ns/op\n"
	c.Assert(output.value, Matches, expected)
}

func (s *BenchmarkS) TestBenchmarkBytes(c *C) {
	helper := FixtureHelper{sleep: 100000}
	output := String{}
	runConf := RunConf{
		Output:        &output,
		Benchmark:     true,
		BenchmarkTime: 10000000,
		Filter:        "Benchmark2",
	}
	Run(&helper, &runConf)

	expected := "PASS: check_test\\.go:[0-9]+: FixtureHelper\\.Benchmark2\t\\s+[0-9]+\t\\s+[0-9]+ ns/op\t\\s+ *[1-9]\\.[0-9]{2} MB/s\n"
	c.Assert(output.value, Matches, expected)
}

func (s *BenchmarkS) TestBenchmarkMem(c *C) {
	helper := FixtureHelper{sleep: 100000}
	output := String{}
	runConf := RunConf{
		Output:        &output,
		Benchmark:     true,
		BenchmarkMem:  true,
		BenchmarkTime: 10000000,
		Filter:        "Benchmark3",
	}
	Run(&helper, &runConf)

	expected := "PASS: check_test\\.go:[0-9]+: FixtureHelper\\.Benchmark3\t\\s+ [0-9]+\t\\s+ *[0-9]+ ns/op\t\\s+ [0-9]+ B/op\t\\s+ [1-9]+ allocs/op\n"
	c.Assert(output.value, Matches, expected)
}
