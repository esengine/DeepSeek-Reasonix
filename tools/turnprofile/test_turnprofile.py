import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import turnprofile as tp  # noqa: E402


def ev(ts, kind, **kw):
    return {"schema_version": 1, "seq": ts, "ts": ts, "event": {"kind": kind, **kw}}


def attempt(ts, action, sid="a", **extra):
    return ev(ts, "stream_attempt", streamAttempt={"id": sid, "action": action, **extra})


def result(ts, start, end, name="bash", tid="t"):
    return ev(ts, "tool_result", tool={"id": tid, "name": name, "startedAt": start, "endedAt": end})


class Partition(unittest.TestCase):
    def one_turn(self):
        return [
            ev(1000, "turn_started"),
            attempt(1100, "begin", "a"),
            attempt(1600, "commit", "a", firstTokenMs=300),
            result(2400, 1600, 2400, tid="x"),
            result(2400, 1600, 2300, tid="y"),
            attempt(2500, "begin", "b"),
            attempt(2600, "discard", "b"),
            attempt(2700, "begin", "c"),
            attempt(3000, "commit", "c", firstTokenMs=100),
            {"schema_version": 1, "seq": 9, "ts": 3050, "turn_completion": True},
        ]

    def test_buckets_partition_the_turn(self):
        (turn,) = tp.profile(self.one_turn())
        self.assertEqual(sum(turn["buckets_ms"].values()), turn["wall_ms"])
        self.assertEqual(turn["wall_ms"], 2050)
        self.assertFalse(turn["censored"])

    def test_each_bucket_reads_its_own_interval(self):
        (turn,) = tp.profile(self.one_turn())
        b = turn["buckets_ms"]
        self.assertEqual(b["host_setup"], 100)
        self.assertEqual(b["model_ttft"], 300 + 100)
        self.assertEqual(b["model_gen"], 200 + 200)
        self.assertEqual(b["tool"], 800)
        self.assertEqual(b["model_discarded"], 100)
        self.assertEqual(b["host_between_rounds"], 100 + 100)
        self.assertEqual(b["host_tail"], 50)

    def test_parallel_tools_count_once_in_wall_and_twice_in_busy(self):
        (turn,) = tp.profile(self.one_turn())
        self.assertEqual(turn["buckets_ms"]["tool"], 800)
        self.assertEqual(turn["tool_busy_ms"], 800 + 700)

    def test_a_turn_with_no_end_is_censored_and_left_out_of_the_aggregate(self):
        turns = tp.profile(self.one_turn()[:-1])
        self.assertTrue(turns[0]["censored"])
        agg = tp.aggregate(turns)
        self.assertEqual((agg["turns"], agg["censored_turns"], agg["wall_ms"]), (0, 1, 0))

    def test_a_tool_overlapping_a_round_goes_to_the_tool(self):
        recs = [
            ev(0, "turn_started"),
            attempt(0, "begin", "a"),
            result(500, 100, 400),
            attempt(1000, "commit", "a"),
            {"schema_version": 1, "seq": 9, "ts": 1000, "turn_completion": True},
        ]
        (turn,) = tp.profile(recs)
        self.assertEqual(turn["buckets_ms"]["tool"], 300)
        self.assertEqual(sum(turn["buckets_ms"].values()), 1000)


if __name__ == "__main__":
    unittest.main()
