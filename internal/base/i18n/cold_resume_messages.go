package i18n

// ColdResumeMessages words the notice a resume through an expired provider cache raises; Messages embeds it.
type ColdResumeMessages struct {
	NoticeColdResumeFmt        string // resumed through an expired cache, no fold offered — %s idle
	NoticeColdResumeCompactFmt string // the same, with a fold offered — %s idle, %s tokens resent
}
