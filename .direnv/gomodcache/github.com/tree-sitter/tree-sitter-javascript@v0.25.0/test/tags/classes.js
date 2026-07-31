class Person {
  //   ^ definition.class
  static foo = bar;

  getName() {
    // ^ definition.method
  }
}

var person = new Person();
//                ^ reference.class

person.getName()
//      ^ reference.call
