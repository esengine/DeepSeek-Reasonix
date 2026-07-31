<?php

function b(int $a, string $b, Person $e): Foo\Dog {}
//         ^^^ @type.builtin
//                 ^^^^^^ @type.builtin
//                            ^^^^^^ @type
//                                        ^^^ @module
//                                            ^^^ @type

function a(array $b) {
//         ^^^^^ @type.builtin
  echo (int) $foo;
//      ^^^ @type.builtin
}

class A {
  public function foo(self $a): self {}
//                    ^^^^ @type.builtin
//                              ^^^^ @type.builtin
  private function baz(): static {}
//                        ^^^^^^ @type.builtin
}
