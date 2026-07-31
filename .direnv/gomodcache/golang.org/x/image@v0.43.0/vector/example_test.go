// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vector_test

import (
	"image"
	"image/draw"
	"os"

	"golang.org/x/image/vector"
)

func Example_draw() {
	const (
		width  = 30
		height = 20
	)

	// Define a closed shape with three edges: two linear and one quadratic.
	// One of its vertices is at the top-left corner of the (1, 2) pixel, which
	// is also the bottom-right corner of the (0, 1) pixel.
	//
	// Co-ordinates can be floating point numbers, not just integers. They can
	// also be outside the vector.Rasterizer's dimensions. The shapes will be
	// clipped during rasterization.
	r := vector.NewRasterizer(width, height)
	r.DrawOp = draw.Src
	r.MoveTo(1, 2)
	r.LineTo(20, 2)
	r.QuadTo(40.5, 15, 10, 20)
	r.ClosePath()

	// Finish the rasterization: the conversion from vector graphics (shapes)
	// to raster graphics (pixels). Co-ordinates are now integers.
	dst := image.NewAlpha(image.Rect(0, 0, width, height))
	r.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})

	// Visualize the pixels.
	const asciiArt = ".++8"
	buf := make([]byte, 0, height*(width+1))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := dst.AlphaAt(x, y).A
			buf = append(buf, asciiArt[a>>6])
		}
		buf = append(buf, '\n')
	}
	os.Stdout.Write(buf)

	// Output:
	// ..............................
	// ..............................
	// .8888888888888888888+.........
	// .+88888888888888888888+.......
	// ..888888888888888888888+......
	// ..+888888888888888888888+.....
	// ...8888888888888888888888+....
	// ...+8888888888888888888888+...
	// ....88888888888888888888888+..
	// ....+88888888888888888888888..
	// .....88888888888888888888888..
	// .....+8888888888888888888888..
	// ......8888888888888888888888..
	// ......+88888888888888888888+..
	// .......8888888888888888888+...
	// .......+88888888888888888.....
	// ........888888888888888+......
	// ........+88888888888+.........
	// .........8888888++............
	// .........+8+++................
}
