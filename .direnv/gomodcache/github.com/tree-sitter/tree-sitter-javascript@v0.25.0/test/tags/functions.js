function foo() {
  //      ^ definition.function
}

foo()
// <- reference.call

{ source: $ => repeat($._expression) }
// ^ definition.function
//              ^ reference.call

let plus1 = x => x + 1
//   ^ definition.function

let plus2 = function(x) { return x + 2 }
//   ^ definition.function

function *gen() { }
//         ^ definition.function

async function* foo() { yield 1; }
//               ^ definition.function
