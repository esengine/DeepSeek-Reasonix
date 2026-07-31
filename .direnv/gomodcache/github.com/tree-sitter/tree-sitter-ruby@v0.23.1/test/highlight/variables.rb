one = 1
# <- variable

def two()
  three = one
  # ^ variable
  #        ^ function.method

  four = three
  # ^ variable
  #       ^ variable

  four.each do |i|
    puts i, three, five
    #    ^ variable.parameter
    #        ^ variable (because blocks are closures)
    #               ^ function.method
  end

  four.each do |(a, b), c: d, e = f|
    #            ^ variable.parameter
    #               ^ variable.parameter
    #                   ^ variable.parameter
    #                      ^ function.method
    #                         ^ variable.parameter
    #                             ^ function.method
    puts a, b, c, d, e, f
    #    ^ variable.parameter
    #       ^ variable.parameter
    #          ^ variable.parameter
    #             ^ function.method
    #                ^ variable.parameter
    #                   ^ function.method
  end

  five ||= 1
  # ^ variable

  six -> (seven) { eight(seven, five) }
  # ^ function.method
  #        ^ variable.parameter
  #                 ^ function.method
  #                      ^ variable.parameter
  #                              ^ variable

  seven
  # ^ function.method (because the `seven` above was in the scope of the lambda)
end
