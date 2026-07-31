//go:build go1.23

package it

import (
	"iter"

	"github.com/samber/lo"
)

// Zip2 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/U5nBWvR8eUZ
func Zip2[A, B any](a iter.Seq[A], b iter.Seq[B]) iter.Seq[lo.Tuple2[A, B]] {
	return func(yield func(lo.Tuple2[A, B]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()

		for {
			var item lo.Tuple2[A, B]
			var ok [2]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			if ok == [2]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip3 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/V5wL9xY8nQr
func Zip3[A, B, C any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C]) iter.Seq[lo.Tuple3[A, B, C]] {
	return func(yield func(lo.Tuple3[A, B, C]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()

		for {
			var item lo.Tuple3[A, B, C]
			var ok [3]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			if ok == [3]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip4 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/W6xM7zZ9oSt
func Zip4[A, B, C, D any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D]) iter.Seq[lo.Tuple4[A, B, C, D]] {
	return func(yield func(lo.Tuple4[A, B, C, D]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()

		for {
			var item lo.Tuple4[A, B, C, D]
			var ok [4]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			if ok == [4]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip5 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/X7yN8aA1pUv
func Zip5[A, B, C, D, E any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E]) iter.Seq[lo.Tuple5[A, B, C, D, E]] {
	return func(yield func(lo.Tuple5[A, B, C, D, E]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()
		nextE, stopE := iter.Pull(e)
		defer stopE()

		for {
			var item lo.Tuple5[A, B, C, D, E]
			var ok [5]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			item.E, ok[4] = nextE()
			if ok == [5]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip6 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/Y4mN8bB2cXw
func Zip6[A, B, C, D, E, F any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F]) iter.Seq[lo.Tuple6[A, B, C, D, E, F]] {
	return func(yield func(lo.Tuple6[A, B, C, D, E, F]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()
		nextE, stopE := iter.Pull(e)
		defer stopE()
		nextF, stopF := iter.Pull(f)
		defer stopF()

		for {
			var item lo.Tuple6[A, B, C, D, E, F]
			var ok [6]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			item.E, ok[4] = nextE()
			item.F, ok[5] = nextF()
			if ok == [6]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip7 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/Z9nA8cC3dXw
func Zip7[A, B, C, D, E, F, G any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G]) iter.Seq[lo.Tuple7[A, B, C, D, E, F, G]] {
	return func(yield func(lo.Tuple7[A, B, C, D, E, F, G]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()
		nextE, stopE := iter.Pull(e)
		defer stopE()
		nextF, stopF := iter.Pull(f)
		defer stopF()
		nextG, stopG := iter.Pull(g)
		defer stopG()

		for {
			var item lo.Tuple7[A, B, C, D, E, F, G]
			var ok [7]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			item.E, ok[4] = nextE()
			item.F, ok[5] = nextF()
			item.G, ok[6] = nextG()
			if ok == [7]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip8 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/0XrQKOk-vw
func Zip8[A, B, C, D, E, F, G, H any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G], h iter.Seq[H]) iter.Seq[lo.Tuple8[A, B, C, D, E, F, G, H]] {
	return func(yield func(lo.Tuple8[A, B, C, D, E, F, G, H]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()
		nextE, stopE := iter.Pull(e)
		defer stopE()
		nextF, stopF := iter.Pull(f)
		defer stopF()
		nextG, stopG := iter.Pull(g)
		defer stopG()
		nextH, stopH := iter.Pull(h)
		defer stopH()

		for {
			var item lo.Tuple8[A, B, C, D, E, F, G, H]
			var ok [8]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			item.E, ok[4] = nextE()
			item.F, ok[5] = nextF()
			item.G, ok[6] = nextG()
			item.H, ok[7] = nextH()
			if ok == [8]bool{} || !yield(item) {
				return
			}
		}
	}
}

// Zip9 creates a sequence of grouped elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/1SmFJ5-zr
func Zip9[A, B, C, D, E, F, G, H, I any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G], h iter.Seq[H], i iter.Seq[I]) iter.Seq[lo.Tuple9[A, B, C, D, E, F, G, H, I]] {
	return func(yield func(lo.Tuple9[A, B, C, D, E, F, G, H, I]) bool) {
		nextA, stopA := iter.Pull(a)
		defer stopA()
		nextB, stopB := iter.Pull(b)
		defer stopB()
		nextC, stopC := iter.Pull(c)
		defer stopC()
		nextD, stopD := iter.Pull(d)
		defer stopD()
		nextE, stopE := iter.Pull(e)
		defer stopE()
		nextF, stopF := iter.Pull(f)
		defer stopF()
		nextG, stopG := iter.Pull(g)
		defer stopG()
		nextH, stopH := iter.Pull(h)
		defer stopH()
		nextI, stopI := iter.Pull(i)
		defer stopI()

		for {
			var item lo.Tuple9[A, B, C, D, E, F, G, H, I]
			var ok [9]bool
			item.A, ok[0] = nextA()
			item.B, ok[1] = nextB()
			item.C, ok[2] = nextC()
			item.D, ok[3] = nextD()
			item.E, ok[4] = nextE()
			item.F, ok[5] = nextF()
			item.G, ok[6] = nextG()
			item.H, ok[7] = nextH()
			item.I, ok[8] = nextI()
			if ok == [9]bool{} || !yield(item) {
				return
			}
		}
	}
}

// ZipBy2 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/2TnGK6-zs
func ZipBy2[A, B, Out any](a iter.Seq[A], b iter.Seq[B], transform func(a A, b B) Out) iter.Seq[Out] {
	return Map(Zip2(a, b), func(item lo.Tuple2[A, B]) Out {
		return transform(item.A, item.B)
	})
}

// ZipBy3 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/3UoHL7-zt
func ZipBy3[A, B, C, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], transform func(a A, b B, c C) Out) iter.Seq[Out] {
	return Map(Zip3(a, b, c), func(item lo.Tuple3[A, B, C]) Out {
		return transform(item.A, item.B, item.C)
	})
}

// ZipBy4 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/4VpIM8-zu
func ZipBy4[A, B, C, D, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], transform func(a A, b B, c C, d D) Out) iter.Seq[Out] {
	return Map(Zip4(a, b, c, d), func(item lo.Tuple4[A, B, C, D]) Out {
		return transform(item.A, item.B, item.C, item.D)
	})
}

// ZipBy5 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/5WqJN9-zv
func ZipBy5[A, B, C, D, E, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], transform func(a A, b B, c C, d D, e E) Out) iter.Seq[Out] {
	return Map(Zip5(a, b, c, d, e), func(item lo.Tuple5[A, B, C, D, E]) Out {
		return transform(item.A, item.B, item.C, item.D, item.E)
	})
}

// ZipBy6 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/6XrKO0-zw
func ZipBy6[A, B, C, D, E, F, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], transform func(a A, b B, c C, d D, e E, f F) Out) iter.Seq[Out] {
	return Map(Zip6(a, b, c, d, e, f), func(item lo.Tuple6[A, B, C, D, E, F]) Out {
		return transform(item.A, item.B, item.C, item.D, item.E, item.F)
	})
}

// ZipBy7 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/7YsLP1-zx
func ZipBy7[A, B, C, D, E, F, G, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G], transform func(a A, b B, c C, d D, e E, f F, g G) Out) iter.Seq[Out] {
	return Map(Zip7(a, b, c, d, e, f, g), func(item lo.Tuple7[A, B, C, D, E, F, G]) Out {
		return transform(item.A, item.B, item.C, item.D, item.E, item.F, item.G)
	})
}

// ZipBy8 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/8isgTsyfL-t
func ZipBy8[A, B, C, D, E, F, G, H, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G], h iter.Seq[H], transform func(a A, b B, c C, d D, e E, f F, g G, h H) Out) iter.Seq[Out] {
	return Map(Zip8(a, b, c, d, e, f, g, h), func(item lo.Tuple8[A, B, C, D, E, F, G, H]) Out {
		return transform(item.A, item.B, item.C, item.D, item.E, item.F, item.G, item.H)
	})
}

// ZipBy9 creates a sequence of transformed elements, the first of which contains the first elements
// of the given sequences, the second of which contains the second elements of the given sequences, and so on.
// When collections are different sizes, the Tuple attributes are filled with zero value.
// Play: https://go.dev/play/p/9jthUzgF-u
func ZipBy9[A, B, C, D, E, F, G, H, I, Out any](a iter.Seq[A], b iter.Seq[B], c iter.Seq[C], d iter.Seq[D], e iter.Seq[E], f iter.Seq[F], g iter.Seq[G], h iter.Seq[H], i iter.Seq[I], transform func(a A, b B, c C, d D, e E, f F, g G, h H, i I) Out) iter.Seq[Out] {
	return Map(Zip9(a, b, c, d, e, f, g, h, i), func(item lo.Tuple9[A, B, C, D, E, F, G, H, I]) Out {
		return transform(item.A, item.B, item.C, item.D, item.E, item.F, item.G, item.H, item.I)
	})
}

// CrossJoin2 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/ZBDEXBYj6nU
func CrossJoin2[A, B any](listA iter.Seq[A], listB iter.Seq[B]) iter.Seq[lo.Tuple2[A, B]] {
	return CrossJoinBy2(listA, listB, lo.T2[A, B])
}

// CrossJoin3 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/0XrQKOk-vw
func CrossJoin3[A, B, C any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C]) iter.Seq[lo.Tuple3[A, B, C]] {
	return CrossJoinBy3(listA, listB, listC, lo.T3[A, B, C])
}

// CrossJoin4 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/1SmFJ5-zr
func CrossJoin4[A, B, C, D any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D]) iter.Seq[lo.Tuple4[A, B, C, D]] {
	return CrossJoinBy4(listA, listB, listC, listD, lo.T4[A, B, C, D])
}

// CrossJoin5 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/2TnGK6-zs
func CrossJoin5[A, B, C, D, E any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E]) iter.Seq[lo.Tuple5[A, B, C, D, E]] {
	return CrossJoinBy5(listA, listB, listC, listD, listE, lo.T5[A, B, C, D, E])
}

// CrossJoin6 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/3UoHL7-zt
func CrossJoin6[A, B, C, D, E, F any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F]) iter.Seq[lo.Tuple6[A, B, C, D, E, F]] {
	return CrossJoinBy6(listA, listB, listC, listD, listE, listF, lo.T6[A, B, C, D, E, F])
}

// CrossJoin7 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/4VpIM8-zu
func CrossJoin7[A, B, C, D, E, F, G any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G]) iter.Seq[lo.Tuple7[A, B, C, D, E, F, G]] {
	return CrossJoinBy7(listA, listB, listC, listD, listE, listF, listG, lo.T7[A, B, C, D, E, F, G])
}

// CrossJoin8 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/5WqJN9-zv
func CrossJoin8[A, B, C, D, E, F, G, H any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G], listH iter.Seq[H]) iter.Seq[lo.Tuple8[A, B, C, D, E, F, G, H]] {
	return CrossJoinBy8(listA, listB, listC, listD, listE, listF, listG, listH, lo.T8[A, B, C, D, E, F, G, H])
}

// CrossJoin9 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/6XrKO0-zw
func CrossJoin9[A, B, C, D, E, F, G, H, I any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G], listH iter.Seq[H], listI iter.Seq[I]) iter.Seq[lo.Tuple9[A, B, C, D, E, F, G, H, I]] {
	return CrossJoinBy9(listA, listB, listC, listD, listE, listF, listG, listH, listI, lo.T9[A, B, C, D, E, F, G, H, I])
}

// CrossJoinBy2 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/7YsLP1-zx
func CrossJoinBy2[A, B, Out any](listA iter.Seq[A], listB iter.Seq[B], transform func(a A, b B) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				if !yield(transform(a, b)) {
					return
				}
			}
		}
	}
}

// CrossJoinBy3 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/8isgTsyfL-t
func CrossJoinBy3[A, B, C, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], transform func(a A, b B, c C) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					if !yield(transform(a, b, c)) {
						return
					}
				}
			}
		}
	}
}

// CrossJoinBy4 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/9jthUzgF-u
func CrossJoinBy4[A, B, C, D, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], transform func(a A, b B, c C, d D) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						if !yield(transform(a, b, c, d)) {
							return
						}
					}
				}
			}
		}
	}
}

// CrossJoinBy5 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/0XrQKOk-vw
func CrossJoinBy5[A, B, C, D, E, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], transform func(a A, b B, c C, d D, e E) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						for e := range listE {
							if !yield(transform(a, b, c, d, e)) {
								return
							}
						}
					}
				}
			}
		}
	}
}

// CrossJoinBy6 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/1SmFJ5-zr
func CrossJoinBy6[A, B, C, D, E, F, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], transform func(a A, b B, c C, d D, e E, f F) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						for e := range listE {
							for f := range listF {
								if !yield(transform(a, b, c, d, e, f)) {
									return
								}
							}
						}
					}
				}
			}
		}
	}
}

// CrossJoinBy7 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/2TnGK6-zs
func CrossJoinBy7[A, B, C, D, E, F, G, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G], transform func(a A, b B, c C, d D, e E, f F, g G) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						for e := range listE {
							for f := range listF {
								for g := range listG {
									if !yield(transform(a, b, c, d, e, f, g)) {
										return
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// CrossJoinBy8 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/3UoHL7-zt
func CrossJoinBy8[A, B, C, D, E, F, G, H, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G], listH iter.Seq[H], transform func(a A, b B, c C, d D, e E, f F, g G, h H) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						for e := range listE {
							for f := range listF {
								for g := range listG {
									for h := range listH {
										if !yield(transform(a, b, c, d, e, f, g, h)) {
											return
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// CrossJoinBy9 combines every item from one list with every item from others.
// It is the cartesian product of lists received as arguments. The transform function
// is used to create the output values.
// Returns an empty list if a list is empty.
// Play: https://go.dev/play/p/4VpIM8-zu
func CrossJoinBy9[A, B, C, D, E, F, G, H, I, Out any](listA iter.Seq[A], listB iter.Seq[B], listC iter.Seq[C], listD iter.Seq[D], listE iter.Seq[E], listF iter.Seq[F], listG iter.Seq[G], listH iter.Seq[H], listI iter.Seq[I], transform func(a A, b B, c C, d D, e E, f F, g G, h H, i I) Out) iter.Seq[Out] {
	return func(yield func(Out) bool) {
		for a := range listA {
			for b := range listB {
				for c := range listC {
					for d := range listD {
						for e := range listE {
							for f := range listF {
								for g := range listG {
									for h := range listH {
										for i := range listI {
											if !yield(transform(a, b, c, d, e, f, g, h, i)) {
												return
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}
