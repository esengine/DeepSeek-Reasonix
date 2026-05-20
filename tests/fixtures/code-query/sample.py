def greet(name: str) -> str:
    return "hi " + name


class User:
    def __init__(self, name: str) -> None:
        self.name = name

    def greet(self) -> str:
        return greet(self.name)


u = User("world")
print(u.greet())
