using namespace std;
// ^ keyword

namespace foo {}
// ^ keyword

template <typename T>
// ^ keyword
//         ^ keyword

class A {
  // <- keyword

public:
  // <- keyword
private:
  // <- keyword
protected:
  // <- keyword
  virtual ~A() = 0;
  // <- keyword
};

int main() {
  throw new Error();
  // ^ keyword
  //     ^ keyword

  try {
    // <- keyword
  } catch (e) {
    // <- keyword
  }
}
