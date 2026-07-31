var a = 'a';
//  ^ variable

var b = function() {};
//  ^ function

var c = () => {};
//  ^ function

var d = async () => {};
//  ^ function

module.e = 'e';
//     ^ property

module.f = function() {};
//     ^ function.method

module.g = async function() {};
//     ^ function.method

module.h = () => {};
//     ^ function.method

function i() {
  //     ^ function
}

class Person {
  static foo = bar;
  //      ^ property

  getName() {
    // ^ function.method
  }
}

foo(function callback() {
  // ^ keyword
  //         ^ function
})


c();
// <- function

module.e();
//     ^ function.method
