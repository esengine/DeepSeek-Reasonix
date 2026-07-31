int main() {
  a<T>();
  // <- function

  a::b();
  // ^ function

  a::b<C, D>();
  // ^ function

  this->b<C, D>();
  //    ^ function

  auto x = y;
  // <- type

  vector<T> a;
  // <- type

  std::vector<T> a;
  //   ^ type
}

class C : D{
  A();
  // <- function

  void efg() {
    // ^ function
  }
}

void A::b() {
  //    ^ function
}
