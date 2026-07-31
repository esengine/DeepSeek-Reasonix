<?php

class A {
  public function foo(self $a): self {
//                          ^ @variable
    new self();
//      ^^^^ @constructor
    new static();
//      ^^^^^^ @constructor
    new parent();
//      ^^^^^^ @constructor
    $this->foo();
//   ^^^^ @variable.builtin
    self::foo();
//  ^^^^ @variable.builtin
    static::foo();
//  ^^^^^^ @variable.builtin
    parent::foo();
//  ^^^^^^ @variable.builtin
  }
}
