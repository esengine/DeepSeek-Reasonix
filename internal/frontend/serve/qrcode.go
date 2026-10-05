package serve

import (
	"fmt"
	"strings"

	"rsc.io/qr"
)

// qrQuiet is the light border a reader needs around the symbol, in modules.
const qrQuiet = 4

// QRSVG draws text as a QR code: one path of unit squares, dark on light, so it
// scans the same in either theme.
func QRSVG(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	side := code.Size + 2*qrQuiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, side, side)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, side, side)
	for y := range code.Size {
		for x := range code.Size {
			if code.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+qrQuiet, y+qrQuiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String(), nil
}
