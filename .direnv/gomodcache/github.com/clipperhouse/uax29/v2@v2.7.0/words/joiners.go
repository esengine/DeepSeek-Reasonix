package words

// Joiners sets runes that should be treated like word characters, where
// otherwise words will be split. See the [Joiners] type.
func (iter *Iterator[T]) Joiners(j *Joiners[T]) {
	iter.Split(j.splitFunc)
}

// Joiners allows specification of characters (runes) which will join words (tokens)
// rather than breaking them. For example, "@" breaks words by default,
// but you might wish to join words into email addresses.
type Joiners[T ~string | ~[]byte] struct {
	// Middle specifies which characters (runes) should
	// join words (tokens) where they would otherwise be split,
	// in the middle of a word.
	//
	// For example, specifying "-" will join hypenated-words.
	// Specifying "@" will preserve email addresses.
	//
	// Note that . (as in "example.com") and ' (as in "it's") are already mid-joiners,
	// specifying them will be redundant and hurt performance.
	Middle []rune

	// Leading specifies which characters (runes) should
	// join words (tokens) where they would otherwise be split,
	// at the beginning of a word.
	//
	// For example, specifying "#" will join #hashtags.
	// Specifying "." will preserve leading decimals like .01.
	Leading []rune
}

func runesContain(runes []rune, rune rune) bool {
	// Did some bechmarking, a map isn't faster for small numbers
	for _, r := range runes {
		if r == rune {
			return true
		}
	}
	return false
}
