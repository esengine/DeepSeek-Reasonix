<?php


echo <<<OMG
  something
OMG;
// <- @string

echo true, TRUE, false, FALSE;
//   ^^^^ @constant.builtin
//         ^^^^ @constant.builtin
//               ^^^^^ @constant.builtin
//                      ^^^^^ @constant.builtin

echo PI_314;
//   ^^^^^^ @constant

echo __DIR__;
//   ^^^^^^^ @constant.builtin
