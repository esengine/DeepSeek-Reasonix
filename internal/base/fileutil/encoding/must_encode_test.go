package encoding

func mustEncode(text string, enc Kind) []byte {
	out, err := Encode(text, enc)
	if err != nil {
		panic(err)
	}
	return out
}
