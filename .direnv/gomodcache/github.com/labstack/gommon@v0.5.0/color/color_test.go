package color

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestText(t *testing.T) {
	Println("*** colored text ***")
	Println(Black("black"))
	Println(Red("red"))
	Println(Green("green"))
	Println(Yellow("yellow"))
	Println(Blue("blue"))
	Println(Magenta("magenta"))
	Println(Cyan("cyan"))
	Println(White("white"))
	Println(Grey("grey"))
}

func TestBackground(t *testing.T) {
	Println("*** colored background ***")
	Println(BlackBg("black background", Wht))
	Println(RedBg("red background"))
	Println(GreenBg("green background"))
	Println(YellowBg("yellow background"))
	Println(BlueBg("blue background"))
	Println(MagentaBg("magenta background"))
	Println(CyanBg("cyan background"))
	Println(WhiteBg("white background"))
}

func TestEmphasis(t *testing.T) {
	Println("*** emphasis ***")
	Println(Reset("reset"))
	Println(Bold("bold"))
	Println(Dim("dim"))
	Println(Italic("italic"))
	Println(Underline("underline"))
	Println(Inverse("inverse"))
	Println(Hidden("hidden"))
	Println(Strikeout("strikeout"))
}

func TestMixMatch(t *testing.T) {
	Println("*** mix and match ***")
	Println(Green("bold green with white background", B, WhtBg))
	Println(Red("underline red", U))
	Println(Yellow("dim yellow", D))
	Println(Cyan("inverse cyan", In))
	Println(Blue("bold underline dim blue", B, U, D))
}

func TestEnableDisable(t *testing.T) {
	Disable()
	assert.Equal(t, "red", Red("red"))
	Enable()
	assert.NotEqual(t, "green", Green("green"))
}
