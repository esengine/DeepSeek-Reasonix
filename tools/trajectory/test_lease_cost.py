import json
import os
import tempfile
import unittest

import lease_cost


def write_log(root, name, frames, truncated=False):
    path = os.path.join(root, name + ".wire.jsonl")
    with open(path, "w", encoding="utf-8") as fh:
        for frame in frames:
            fh.write(json.dumps(frame) + "\n")
    if truncated:
        with open(os.path.join(root, name + ".wire.meta.json"), "w", encoding="utf-8") as fh:
            json.dump({"truncated": True}, fh)
    return path


def lease(**fields):
    return {"kind": "workspace_lease", "workspaceLease": fields}


class TestCollection(unittest.TestCase):
    def test_reads_only_lease_frames(self):
        with tempfile.TemporaryDirectory() as root:
            path = write_log(root, "s1", [
                {"kind": "turn_started"},
                lease(contended=0, heldMs=10, idleMs=4),
                {"kind": "tool_result"},
                lease(contended=1, waitedMs=200, heldMs=50, idleMs=50),
            ])
            self.assertEqual(len(lease_cost.holds(path)), 2)

    def test_a_partial_last_line_is_skipped(self):
        with tempfile.TemporaryDirectory() as root:
            path = os.path.join(root, "s1.wire.jsonl")
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(json.dumps(lease(contended=0, heldMs=10, idleMs=1)) + "\n")
                fh.write('{"kind":"workspace_lea')
            self.assertEqual(len(lease_cost.holds(path)), 1)

    def test_finds_logs_under_a_directory(self):
        with tempfile.TemporaryDirectory() as root:
            nested = os.path.join(root, "a", "b")
            os.makedirs(nested)
            write_log(nested, "s1", [lease(contended=0, heldMs=1, idleMs=0)])
            write_log(root, "s2", [lease(contended=0, heldMs=1, idleMs=0)])
            with open(os.path.join(root, "notes.txt"), "w", encoding="utf-8") as fh:
                fh.write("x")
            self.assertEqual(len(lease_cost.wire_logs(root)), 2)


class TestTruncation(unittest.TestCase):
    def test_absent_witness_means_intact(self):
        with tempfile.TemporaryDirectory() as root:
            path = write_log(root, "s1", [lease(contended=0, heldMs=1, idleMs=0)])
            self.assertFalse(lease_cost.truncated(path))

    def test_a_witness_is_reported(self):
        with tempfile.TemporaryDirectory() as root:
            path = write_log(root, "s1", [lease(contended=0, heldMs=1, idleMs=0)], truncated=True)
            self.assertTrue(lease_cost.truncated(path))

    # An unreadable witness may not be reported as an intact log: the whole
    # point of the record is that what is missing biases the numbers.
    def test_an_unreadable_witness_counts_as_truncated(self):
        with tempfile.TemporaryDirectory() as root:
            path = write_log(root, "s1", [lease(contended=0, heldMs=1, idleMs=0)])
            with open(os.path.join(root, "s1.wire.meta.json"), "w", encoding="utf-8") as fh:
                fh.write("{not json")
            self.assertTrue(lease_cost.truncated(path))


class TestSummary(unittest.TestCase):
    def summary(self, frames):
        with tempfile.TemporaryDirectory() as root:
            write_log(root, "s1", frames)
            return lease_cost.summarise(root)

    def test_counts_every_hold_and_only_contended_waits(self):
        s = self.summary([
            lease(contended=0, heldMs=100, idleMs=10),
            lease(contended=2, reported=1, waitedMs=3000, heldMs=200, idleMs=20),
        ])
        self.assertEqual(s["holds"], 2)
        self.assertEqual(s["contended_holds"], 1)
        self.assertEqual(s["reported_holds"], 1)
        self.assertEqual(s["waited_ms_max"], 3000)

    # The denominator is every hold, not every contended one: a session that
    # never waited is most of what the rate is a rate of.
    def test_an_uncontended_run_reports_no_wait_at_all(self):
        s = self.summary([lease(contended=0, heldMs=100, idleMs=50)])
        self.assertEqual(s["contended_holds"], 0)
        self.assertIsNone(s["waited_ms_p50"])
        self.assertEqual(s["idle_share_p50"], 0.5)

    # A zero-length hold has no share, and reporting 0 for one would read as a
    # hold measured to have been busy throughout.
    def test_a_zero_length_hold_has_no_share(self):
        s = self.summary([lease(contended=0, heldMs=0, idleMs=0)])
        self.assertEqual(s["holds"], 1)
        self.assertIsNone(s["idle_share_p50"])

    def test_nothing_recorded_says_so(self):
        with tempfile.TemporaryDirectory() as root:
            s = lease_cost.summarise(root)
            self.assertEqual(s["holds"], 0)
            self.assertIn("Nothing above is a measurement", lease_cost.render(s))

    def test_a_truncated_log_is_said_in_the_rendering(self):
        with tempfile.TemporaryDirectory() as root:
            write_log(root, "s1", [lease(contended=0, heldMs=10, idleMs=1)], truncated=True)
            out = lease_cost.render(lease_cost.summarise(root))
            self.assertIn("floor", out)


class TestPercentile(unittest.TestCase):
    def test_empty_has_none(self):
        self.assertIsNone(lease_cost.percentile([], 0.5))

    def test_nearest_rank(self):
        self.assertEqual(lease_cost.percentile([1, 2, 3, 4], 0.5), 2)
        self.assertEqual(lease_cost.percentile([1, 2, 3, 4], 0.9), 4)
        self.assertEqual(lease_cost.percentile([5], 0.9), 5)


if __name__ == "__main__":
    unittest.main()
