from dataclasses import dataclass

@dataclass
class User:
    name: str
    email: str

    def greeting(self):
        # BUG 1: self.username doesn't exist
        return f"Hello, {self.username}!"
