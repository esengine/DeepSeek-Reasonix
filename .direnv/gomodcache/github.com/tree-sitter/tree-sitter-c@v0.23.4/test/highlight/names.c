typedef struct {
  // ^ keyword
  //    ^ keyword
  a_t b;
  // <- type
  //  ^ property

  unsigned c_t (*d)[2];
  // ^ type
  //       ^ type
  //             ^ property
}, T, V;
// ^ type
//    ^ type

int main(const char string[SIZE]) {
// <- type
//  ^ function
//        ^ keyword
//             ^ type
//                   ^ variable
//                         ^ constant

  return foo.bar + foo.baz();
  // ^ keyword
  //     ^ variable
  //         ^ property
  //                   ^ function

error:
  // <- label
  return 0;
}
