<?php

namespace A\B;
//        ^ definition.module
//          ^ definition.module

interface T {
//        ^ definition.interface
}

trait U {
//    ^ definition.interface
}

function f() {
//       ^ definition.function
}

class A {
//    ^ definition.class
    int $foo;
//       ^^^ definition.field
    public function a() {
//                  ^ definition.function
    }
}
