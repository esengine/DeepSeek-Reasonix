class A {}
//    ^ constructor
const ABC = 1
//     ^ constant
const AB_C1 = 2
//    ^ constant
const {AB_C2_D3} = x
//      ^ constant

module.exports = function(one, two) {
  // <- variable.builtin
  //                      ^ variable.parameter

  if (something()) {
    let module = null, one = 1;
    //  ^ variable
    //                 ^ variable

    console.log(module, one, two);
    // ^ variable.builtin
    //            ^ variable
    //                   ^ variable
    //                        ^ variable.parameter
  }

  console.log(module, one, two);
  // ^ variable.builtin
  //            ^ variable.builtin
  //                   ^ variable.parameter
  //                        ^ variable.parameter
};

console.log(module, one, two);
// ^ variable.builtin
//            ^ variable.builtin
//                   ^ variable
//                        ^ variable

function one({two: three}, [four]) {
  //          ^ property
  //               ^ variable.parameter
  //                         ^ variable.parameter

  console.log(two, three, four)
  //           ^ variable
  //                ^ variable.parameter
  //                       ^ variable.parameter
}
