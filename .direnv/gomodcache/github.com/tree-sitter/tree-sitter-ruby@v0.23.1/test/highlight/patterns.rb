expr = [false | __LINE__ | __FILE__ | __ENCODING__]
#       ^ constant.builtin
#               ^ constant.builtin
#                          ^ constant.builtin
#                                     ^ constant.builtin

case expr
  in {a: 5, b:, **nil}
#               ^ operator
#                 ^ constant.builtin
  in [false | __LINE__ | __FILE__ | __ENCODING__]
#     ^ constant.builtin
#             ^ constant.builtin
#                        ^ constant.builtin
#                                   ^ constant.builtin
  else
end

