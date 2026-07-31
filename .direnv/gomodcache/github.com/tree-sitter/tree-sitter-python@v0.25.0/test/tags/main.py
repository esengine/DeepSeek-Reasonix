class MyClass:
  #    ^ definition.class
  def hello():
    #  ^ definition.function
    print "hello from MyClass"

MyClass.hello()
#        ^ reference.call

def main():
  #  ^ definition.function
  print "Hello, world!"

main()
# <- reference.call
