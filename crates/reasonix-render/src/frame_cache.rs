/// Frame identity cache — skips deserialization and rendering when consecutive
/// frames are byte-identical. The trace scene emitted by useSceneTrace only
/// changes when cards, model, or activity label change, so most frames between
/// streaming deltas are duplicates.
///
/// Ratatui's Terminal::draw already diffs cells against the previous terminal
/// state before emitting ANSI. This cache operates one layer above: it avoids
/// the JSON parse + scene render + ratatui diff pipeline entirely for
/// unchanged frames, saving CPU on both the JS and Rust sides of the bridge.
#[derive(Default)]
pub struct FrameCache {
    /// Raw JSON line from the previous frame.
    prev_raw: String,
    /// Number of consecutive skipped frames.
    skipped: u64,
    /// False until the first frame is seen — guarantees is_new returns true for frame 0.
    has_prev: bool,
}

impl FrameCache {
    /// Returns true if this raw JSON line differs from the previous one
    /// (or if no previous frame exists). False means the frame is a
    /// byte-for-byte duplicate and can be skipped entirely.
    pub fn is_new(&mut self, raw: &str) -> bool {
        if !self.has_prev {
            self.prev_raw.clear();
            self.prev_raw.push_str(raw);
            self.has_prev = true;
            return true;
        }
        if raw == self.prev_raw {
            self.skipped = self.skipped.saturating_add(1);
            return false;
        }
        self.prev_raw.clear();
        self.prev_raw.push_str(raw);
        self.skipped = 0;
        true
    }

    /// Number of consecutive frames skipped since the last rendered frame.
    pub fn skipped_count(&self) -> u64 {
        self.skipped
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn first_frame_is_always_new() {
        let mut cache = FrameCache::default();
        assert!(cache.is_new(r#"{"v":1}"#));
    }

    #[test]
    fn identical_frame_is_skipped() {
        let mut cache = FrameCache::default();
        assert!(cache.is_new(r#"{"v":1}"#));
        assert!(!cache.is_new(r#"{"v":1}"#));
        assert_eq!(cache.skipped_count(), 1);
    }

    #[test]
    fn changed_frame_is_not_skipped() {
        let mut cache = FrameCache::default();
        assert!(cache.is_new(r#"{"v":1}"#));
        assert!(cache.is_new(r#"{"v":2}"#));
        assert_eq!(cache.skipped_count(), 0);
    }

    #[test]
    fn empty_string_is_valid_frame() {
        let mut cache = FrameCache::default();
        // Empty lines are filtered before reaching the cache in main.rs,
        // but if one arrives it should be treated normally.
        assert!(cache.is_new(""));
        assert!(!cache.is_new(""));
    }
}
