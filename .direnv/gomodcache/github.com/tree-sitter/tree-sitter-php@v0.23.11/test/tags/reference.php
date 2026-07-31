<?php

class B implements T {
//                 ^ reference.implementation
}

new A\B();
//    ^ reference.class

new $foo();
//   ^^^ reference.class

$bar->f();
//    ^ reference.call

\f();
#^ reference.call

C::f();
// ^ reference.call
