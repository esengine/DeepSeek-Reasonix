<?php
// <- @tag

declare(strict_types=1);
// <- @keyword

include "file.php";
// <- @keyword
include_once "file.php";
// <- @keyword
require "file.php";
// <- @keyword
require_once "file.php";
// <- @keyword

namespace A\B;
//^^^^^^^ @keyword
//        ^ @module
//          ^ @module

use Foo\Baz as Baaz;
//^ @keyword
//  ^^^ @module
//      ^^^ @type
//          ^^ @keyword
//             ^^^^ @type

use function Foo\foo as fooo;
//  ^^^^^^^^ @keyword
//               ^^^ @function
//                      ^^^^ @function

use const Foo\FOO as FOOO;
//  ^^^^^ @keyword
//            ^^^ @constant
//                   ^^^^ @constant

use Foo\Baz\{
//  ^^^ @module
//      ^^^ @module
  Bar,
//^^^ @type
  function foo,
//^^^^^^^^ @keyword
//         ^^^ @function
  const FOO,
//^^^^^ @keyword
//      ^^^ @constant
};

if ($a and $b or $c xor $d) {} elseif ($b) {} else {}
// <- @keyword
//     ^^^ @keyword
//            ^^ @keyword
//                  ^^^ @keyword
//                             ^^^^^^ @keyword
//                                            ^^^^ @keyword

for ($i = 0; $i < 1; $i++) { continue; }
// <- @keyword
//                           ^^^^^^^^ @keyword

while ($b) {}
// <- @keyword

WHILE ($b) {}
// <- @keyword

do { } while ($c);
// <- @keyword
//     ^^^^^ @keyword

foreach ($foos as $foo) {}
// <- @keyword
//             ^^ @keyword

try {} catch (Exception $e) {} finally {}
// <- @keyword
//     ^^^^^ @keyword
//                             ^^^^^^^ @keyword

function a() {}
// <- @keyword

static function a() {}
// <- @keyword

static function () {}
// <- @keyword

static fn () => 1;
// <- @keyword

abstract class A
// <- @keyword
//       ^^^^^ @keyword
{
  private const BAR = 1;
//^^^^^^^ @keyword
//        ^^^^^ @keyword
  protected readonly static $a;
//^^^^^^^^^ @keyword
//          ^^^^^^^^ @keyword
//                   ^^^^^^ @keyword
  final public $b;
//^^^^^ @keyword
  public static function foo(): static {}
//^^^^^^ @keyword
//       ^^^^^^ @keyword
//              ^^^^^^^^ @keyword
}

class B extends A implements T
//      ^^^^^^^ @keyword
//                ^^^^^^^^^^ @keyword
{
  use T, U {
//^^^ @keyword
    U::small insteadof T;
//           ^^^^^^^^^ @keyword
  }
  public function foo(callable $call): self
  {
    $call instanceof Closure;
//        ^^^^^^^^^^ @keyword
    fn ($a, $b) => $a + $b;
//  ^^ @keyword
    static $a;
//  ^^^^^^ @keyword
    global $a;
//  ^^^^^^ @keyword
    clone $call;
//  ^^^^^ @keyword
    match ($a) {
//  ^^^^^ @keyword
      default => "other",
//    ^^^^^^^ @keyword
    };

    switch ($a) {
//  ^^^^^^ @keyword
      case 'value':
//    ^^^^ @keyword
        break;
//      ^^^^^ @keyword
      default:
//    ^^^^^^^ @keyword
    }
    yield $a;
//  ^^^^^ @keyword
    yield from $a;
//        ^^^^ @keyword
    return $a;
//  ^^^^^^ @keyword
    goto a;
//  ^^^^ @keyword
    echo "a";
//  ^^^^ @keyword
    print "a";
//  ^^^^^ @keyword
    print("a");
//  ^^^^^ @keyword
    exit;
//  ^^^^ @keyword
    exit();
//  ^^^^ @function.builtin
    exit(1);
//  ^^^^ @function.builtin
  }
}

throw new Exception("oh");
// <- @keyword
//    ^^^ @keyword

interface T {}
// <- @keyword

trait T { public function small(): void {} }
// <- @keyword
trait U { public function small(): void {} }
// <- @keyword
enum Foo { case Bar; }
//^^ @keyword
//         ^^^^  @keyword
