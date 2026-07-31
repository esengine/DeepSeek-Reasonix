require "a"
# ^ function.method.builtin

class Car < Vehicle
  # <- keyword
  #    ^ constructor

  def init(id)
    # <- keyword
    # ^ function.method

    @id = id
    # <- property
    #     ^ variable.parameter

    yield
    # <- keyword
    return
    # <- keyword
    next
    # <- keyword
  end

  private
  # ^ keyword

  public
  # ^ keyword

  protected
  # ^ keyword
end
# <- keyword
